// Package panelapi — REST для админ-панели (docs/07-api.md §2), вход и сессии, SSE, раздача собранной панели.
package panelapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"fuelwatch/internal/health"
	"fuelwatch/internal/httpx"
	"fuelwatch/internal/notify"
	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
	"fuelwatch/internal/stream"
	"fuelwatch/internal/vision"
	"fuelwatch/internal/visits"
)

const (
	cookieName      = "fw_session"
	sessionTTL      = 30 * 24 * time.Hour
	sessionRenewGap = 24 * time.Hour // продлевать не чаще раза в сутки
	loginPerMinute  = 10
	maxJSONBody     = 256 << 10
	csrfHeader      = "X-Requested-With"
	csrfValue       = "fuelwatch"
)

type API struct {
	DB           *pgxpool.Pool
	Settings     *settings.Service
	Notify       *notify.Service
	Health       *health.Service
	Hub          *stream.Hub
	Visits       *visits.Service
	Classifier   vision.Classifier
	FramesDir    string
	SecureCookie bool

	loginLimit httpx.Limiter
}

type User struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type ctxKey struct{}

func userFrom(r *http.Request) *User { return r.Context().Value(ctxKey{}).(*User) }

func (a *API) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(csrf)
	r.Post("/auth/login", a.login)
	r.Group(func(r chi.Router) {
		r.Use(a.requireSession)
		r.Post("/auth/logout", a.logout)
		r.Get("/auth/me", func(w http.ResponseWriter, r *http.Request) { httpx.OK(w, userFrom(r)) })
		r.Get("/stream", a.Hub.ServeHTTP)

		r.Get("/devices", a.listDevices)
		r.Post("/devices", a.createDevice)
		r.Get("/devices/{id}", a.getDevice)
		r.Patch("/devices/{id}", a.patchDevice)
		r.Delete("/devices/{id}", a.deleteDevice)
		r.Post("/devices/{id}/pairing-code", a.newPairingCode)
		r.Post("/devices/{id}/mode", a.setMode)
		r.Post("/devices/{id}/commands", a.createCommand)
		r.Get("/devices/{id}/commands", a.listCommands)
		r.Get("/devices/{id}/events", a.listEvents)
		r.Get("/devices/{id}/settings", a.getDeviceSettings)
		r.Post("/devices/{id}/live", a.setLive)
		r.Post("/devices/{id}/reference", a.setReference)
		r.Get("/devices/{id}/zones", a.getZones)
		r.Put("/devices/{id}/zones", a.putZones)
		r.Get("/devices/{id}/zones/versions", a.listZoneVersions)
		r.Post("/devices/{id}/zones/restore", a.restoreZones)
		r.Get("/devices/{id}/timeline", a.timeline)
		r.Get("/devices/{id}/heartbeats", a.heartbeatSeries)
		r.Get("/devices/{id}/log", a.deviceLog)
		r.Get("/devices/{id}/issues", a.listIssues)
		r.Post("/issues/{id}/close", a.closeIssue)
		r.Put("/devices/{id}/settings", a.putDeviceSettings)

		r.Get("/frames", a.listFrames)
		r.Get("/frames/{id}", a.getFrame)
		r.Get("/frames/{id}/image", a.frameFile(false))
		r.Get("/frames/{id}/thumb", a.frameFile(true))
		r.Post("/frames/{id}/classify", a.classifyFrame)

		r.Get("/visits", a.listVisits)
		r.Get("/visits/{id}", a.getVisit)
		r.Post("/visits/{id}/feedback", a.visitFeedback)
		r.Post("/visits/{id}/close", a.closeVisit)

		r.Get("/users", a.listUsers)
		r.Post("/users", a.createUser)
		r.Patch("/users/{id}", a.patchUser)

		r.Get("/costs", a.costs)
		r.Get("/vision/providers", a.visionProviders)
		r.Get("/notify/telegram", a.telegramStatus)
		r.Post("/notify/telegram/test", a.telegramTest)

		r.Get("/notifications", a.listNotifications)
		r.Get("/notifications/unread-count", a.unreadCount)
		r.Post("/notifications/read-all", a.readAll)
		r.Post("/notifications/{id}/read", a.readOne)

		r.Get("/settings/schema", a.settingsSchema)
		r.Get("/settings/global", a.getGlobalSettings)
		r.Put("/settings/global", a.putGlobalSettings)
	})
	return r
}

// csrf: изменяющие запросы должны нести X-Requested-With: fuelwatch (браузер не пошлёт его с чужого сайта без CORS).
func csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(csrfHeader) != csrfValue {
			httpx.Error(w, http.StatusForbidden, "csrf", "missing "+csrfHeader)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "login required")
			return
		}
		ctx := r.Context()
		sid := store.HashToken(c.Value)
		var u User
		var expires time.Time
		err = a.DB.QueryRow(ctx, `SELECT u.id, u.login, s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id
			WHERE s.id=$1 AND s.expires_at > now() AND u.disabled_at IS NULL`, sid).Scan(&u.ID, &u.Login, &expires)
		if store.IsNoRows(err) {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "session expired")
			return
		}
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		if time.Until(expires) < sessionTTL-sessionRenewGap {
			if _, err := a.DB.Exec(ctx, `UPDATE sessions SET expires_at=$2 WHERE id=$1`, sid, time.Now().Add(sessionTTL)); err == nil {
				a.setCookie(w, c.Value, sessionTTL)
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, ctxKey{}, &u)))
	})
}

func (a *API) setCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: value, Path: "/", HttpOnly: true, Secure: a.SecureCookie,
		SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds()),
	})
}

// dummyHash — чтобы время ответа не выдавало, существует ли логин.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	a.loginLimit.PerMinute = loginPerMinute
	if !a.loginLimit.Allow(httpx.ClientIP(r)) {
		httpx.TooMany(w)
		return
	}
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	ctx := r.Context()
	var u User
	var hash string
	err := a.DB.QueryRow(ctx, `SELECT id, login, password_hash FROM users WHERE login=$1 AND disabled_at IS NULL`,
		strings.TrimSpace(req.Login)).Scan(&u.ID, &u.Login, &hash)
	if err != nil && !store.IsNoRows(err) {
		httpx.Internal(w, r, err)
		return
	}
	if err != nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Password))
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "неверный логин или пароль")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "неверный логин или пароль")
		return
	}
	token := store.RandomToken(32)
	ua := r.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	if _, err := a.DB.Exec(ctx, `INSERT INTO sessions (id, user_id, expires_at, user_agent) VALUES ($1, $2, $3, $4)`,
		store.HashToken(token), u.ID, time.Now().Add(sessionTTL), ua); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.DB.Exec(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, u.ID)
	a.setCookie(w, token, sessionTTL)
	httpx.OK(w, u)
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		a.DB.Exec(r.Context(), `DELETE FROM sessions WHERE id=$1`, store.HashToken(c.Value))
	}
	a.setCookie(w, "", -time.Second)
	httpx.OK(w, map[string]bool{"ok": true})
}

var weakAdminPasswords = map[string]bool{"change-me": true, "change-me-too": true, "admin": true, "password": true}

// EnsureAdmin создаёт первого пользователя из env, если пользователей нет.
func EnsureAdmin(ctx context.Context, db *pgxpool.Pool, login, password string) (bool, error) {
	if login == "" || password == "" {
		return false, nil
	}
	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil || exists {
		return false, err
	}
	// Репозиторий публичный: пароль из примера .env известен всем — с ним первого админа не создаём.
	if weakAdminPasswords[password] || len([]rune(password)) < minPassword {
		return false, fmt.Errorf("ADMIN_PASSWORD: задайте свой пароль не короче %d символов (не из .env.example)", minPassword)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, err
	}
	tag, err := db.Exec(ctx, `INSERT INTO users (login, password_hash) SELECT $1, $2
		WHERE NOT EXISTS (SELECT 1 FROM users)`, login, string(hash))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
