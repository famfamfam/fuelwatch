// fuelwatch — сервер FuelWatch: API телефона, API и страница панели, фоновые задачи.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"fuelwatch/internal/deviceapi"
	"fuelwatch/internal/health"
	"fuelwatch/internal/notify"
	"fuelwatch/internal/panelapi"
	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
	"fuelwatch/internal/stream"
	"fuelwatch/internal/telegram"
	"fuelwatch/internal/tgbot"
	"fuelwatch/internal/vision"
	"fuelwatch/internal/vision/llm"
	"fuelwatch/internal/visits"
	"fuelwatch/internal/worker"
)

// newClassifier — VLM через llm.Client выбранного провайдера (vision.provider); ключ и адрес — из env.
func newClassifier(s *settings.Service, publicURL string) *vision.VLMClassifier {
	cfg := llm.ProviderConfig{
		BaseURL: env("VLM_BASE_URL", "https://openrouter.ai/api/v1"),
		APIKey:  os.Getenv("VLM_API_KEY"),
		Referer: publicURL,
		Title:   "FuelWatch",
	}
	var mu sync.Mutex
	clients := map[string]llm.Client{}
	return &vision.VLMClassifier{Settings: s, Client: func(provider string) (llm.Client, error) {
		mu.Lock()
		defer mu.Unlock()
		if c, ok := clients[provider]; ok {
			return c, nil
		}
		c, err := llm.New(provider, cfg)
		if err == nil {
			clients[provider] = c
		}
		return c, err
	}}
}

const (
	dbWait          = 60 * time.Second
	shutdownTimeout = 10 * time.Second
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	if len(os.Args) > 1 && os.Args[1] == "replay" {
		if err := runReplay(os.Args[2:]); err != nil {
			slog.Error("replay", "err", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	framesDir := env("FRAMES_DIR", "./data/frames")
	listen := env("LISTEN_ADDR", ":8080")
	publicURL := os.Getenv("PUBLIC_URL")

	if err := os.MkdirAll(framesDir, 0o755); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, dbURL, dbWait)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(pool); err != nil {
		return err
	}
	created, err := panelapi.EnsureAdmin(ctx, pool, os.Getenv("ADMIN_LOGIN"), os.Getenv("ADMIN_PASSWORD"))
	if err != nil {
		return err
	}
	if created {
		slog.Info("first admin created", "login", os.Getenv("ADMIN_LOGIN"))
	}

	if m := os.Getenv("VLM_MODEL"); m != "" {
		settings.SetDefault("vision.model", m)
	}

	hub := stream.New()
	settingsSvc := &settings.Service{DB: pool}
	var tg *telegram.Client
	if tok := os.Getenv("TELEGRAM_BOT_TOKEN"); tok != "" {
		tg = telegram.New(tok, os.Getenv("TELEGRAM_API_URL"))
	}
	notifySvc := &notify.Service{DB: pool, Hub: hub, Settings: settingsSvc, Telegram: tg, FramesDir: framesDir, PublicURL: publicURL}
	healthSvc := &health.Service{DB: pool, Settings: settingsSvc, Notify: notifySvc, Hub: hub}
	visitsSvc := &visits.Service{DB: pool, Notify: notifySvc, Hub: hub, Settings: settingsSvc}
	cleanup := &worker.Cleanup{DB: pool, Settings: settingsSvc, FramesDir: framesDir}
	classifier := newClassifier(settingsSvc, publicURL)
	visionWorker := &worker.Vision{DB: pool, Classifier: classifier, Settings: settingsSvc, Visits: visitsSvc, Health: healthSvc, Hub: hub, FramesDir: framesDir}
	var bot *tgbot.Bot
	if tg != nil {
		bot = &tgbot.Bot{TG: tg, DB: pool, Settings: settingsSvc, Visits: visitsSvc, Hub: hub, FramesDir: framesDir}
	}

	dev := &deviceapi.API{DB: pool, Settings: settingsSvc, Health: healthSvc, Hub: hub, FramesDir: framesDir,
		OnFrame: func(ctx context.Context, deviceID, frameID, kind string, commandID *string) {
			visionWorker.Wake()
			if bot != nil {
				go bot.OnFrame(ctx, deviceID, frameID, commandID)
			}
		}}
	panel := &panelapi.API{
		DB: pool, Settings: settingsSvc, Notify: notifySvc, Health: healthSvc, Hub: hub,
		Visits: visitsSvc, Classifier: classifier,
		FramesDir: framesDir, SecureCookie: strings.HasPrefix(publicURL, "https://"),
	}

	r := chi.NewRouter()
	r.Use(middleware.RealIP, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "db: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	r.Mount("/api/device", dev.Routes())
	r.Mount("/api/panel", panel.Routes())
	r.Handle("/api/*", http.NotFoundHandler())
	r.Handle("/*", panelapi.Static())

	go healthSvc.Run(ctx)
	go cleanup.Run(ctx)
	go visitsSvc.Run(ctx)
	go visionWorker.Run(ctx)
	if bot != nil {
		go bot.Run(ctx)
		slog.Info("telegram bot started")
	}

	// WriteTimeout не задаём: SSE-соединения живут долго. BaseContext отменяет их при остановке.
	srv := &http.Server{
		Addr: listen, Handler: r, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", listen)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutCtx)
}
