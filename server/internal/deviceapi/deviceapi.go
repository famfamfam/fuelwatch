// Package deviceapi — API телефона (docs/07-api.md §1). Авторизация по токену устройства.
package deviceapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fuelwatch/internal/health"
	"fuelwatch/internal/httpx"
	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
	"fuelwatch/internal/stream"
)

const (
	maxJSONBody  = 256 << 10
	maxFrameBody = 12 << 20
	// Код привязки — 8 символов из 31 и живёт час: при 10 попытках в минуту с адреса перебор бесполезен.
	pairPerMinute = 10
)

var frameKinds = []string{"keyframe", "change", "snapshot", "reference"}

type API struct {
	DB        *pgxpool.Pool
	Settings  *settings.Service
	Health    *health.Service
	Hub       *stream.Hub
	FramesDir string
	pairLimit httpx.Limiter
	// OnFrame вызывается после сохранения кадра: будит VLM-воркер, отвечает на снимок из Telegram.
	OnFrame func(ctx context.Context, deviceID, frameID, kind string, commandID *string)
}

func (a *API) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/pair", a.pair)
	r.Group(func(r chi.Router) {
		r.Use(a.auth)
		r.Post("/heartbeat", a.heartbeat)
		r.Post("/frames", a.frames)
		r.Post("/events", a.events)
	})
	return r
}

type ctxKey struct{}

func deviceFrom(r *http.Request) *store.Device { return r.Context().Value(ctxKey{}).(*store.Device) }

func (a *API) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || tok == "" {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "missing token")
			return
		}
		d, err := store.DeviceByTokenHash(r.Context(), a.DB, store.HashToken(tok))
		if err == store.ErrNotFound {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "unknown token")
			return
		}
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		if d.DisabledAt != nil {
			httpx.Error(w, http.StatusForbidden, "device_disabled", "device disabled")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, d)))
	})
}

// NormalizeCode — код привязки без дефисов и пробелов, в верхнем регистре.
func NormalizeCode(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(s)))
}

func (a *API) pair(w http.ResponseWriter, r *http.Request) {
	a.pairLimit.PerMinute = pairPerMinute
	if !a.pairLimit.Allow(httpx.ClientIP(r)) {
		httpx.TooMany(w)
		return
	}
	var req struct {
		Code       string `json:"code"`
		Model      string `json:"model"`
		Android    string `json:"android"`
		AppVersion string `json:"app_version"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	ctx := r.Context()
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	var deviceID string
	err = tx.QueryRow(ctx, `SELECT p.device_id FROM pairing_codes p JOIN devices d ON d.id=p.device_id
		WHERE p.code_hash=$1 AND p.used_at IS NULL AND p.expires_at > now() AND d.disabled_at IS NULL
		FOR UPDATE OF p`, store.HashToken(NormalizeCode(req.Code))).Scan(&deviceID)
	if store.IsNoRows(err) {
		httpx.Error(w, http.StatusBadRequest, "invalid_code", "код не найден или истёк")
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	token := "fw_" + store.RandomToken(32)
	if _, err := tx.Exec(ctx, `UPDATE pairing_codes SET used_at=now() WHERE code_hash=$1`, store.HashToken(NormalizeCode(req.Code))); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	// Новая привязка сбрасывает версию конфига на телефоне: он получит полный конфиг в первом heartbeat.
	if _, err := tx.Exec(ctx, `UPDATE devices SET token_hash=$2, model=$3, android=$4, app_version=$5, device_config_version=0
		WHERE id=$1`, deviceID, store.HashToken(token), req.Model, req.Android, req.AppVersion); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	slog.Info("device paired", "device", deviceID, "model", req.Model)
	a.publishStatus(ctx, deviceID)
	httpx.OK(w, map[string]any{"device_id": deviceID, "token": token, "server_time": time.Now().UTC()})
}

type commandDone struct {
	ID    string  `json:"id"`
	OK    bool    `json:"ok"`
	Error *string `json:"error"`
}

type heartbeatReq struct {
	ConfigVersion int             `json:"config_version"`
	AppVersion    string          `json:"app_version"`
	CameraCaps    json.RawMessage `json:"camera_caps"`
	CommandsDone  []commandDone   `json:"commands_done"`
}

type commandOut struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Params    json.RawMessage `json:"params"`
	ExpiresAt time.Time       `json:"expires_at"`
}

type deviceConfig struct {
	ConfigVersion int             `json:"config_version"`
	Mode          string          `json:"mode"`
	LiveUntil     *time.Time      `json:"live_until"`
	Zones         json.RawMessage `json:"zones"`
	Settings      map[string]any  `json:"settings"`
}

func (a *API) heartbeat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := deviceFrom(r)
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var req heartbeatReq
	if err := json.Unmarshal(raw, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var caps []byte
	if len(req.CameraCaps) > 0 && string(req.CameraCaps) != "null" {
		caps = req.CameraCaps
	}

	_, err = a.DB.Exec(ctx, `UPDATE devices SET last_seen_at=now(), last_heartbeat=$2, device_config_version=$3,
		app_version=COALESCE(NULLIF($4, ''), app_version), camera_caps=COALESCE($5::jsonb, camera_caps) WHERE id=$1`,
		d.ID, raw, req.ConfigVersion, req.AppVersion, caps)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if _, err := a.DB.Exec(ctx, `INSERT INTO heartbeats (device_id, at, body) VALUES ($1, now(), $2)`, d.ID, raw); err != nil {
		httpx.Internal(w, r, err)
		return
	}

	for _, cd := range req.CommandsDone {
		c, err := store.CompleteCommand(ctx, a.DB, d.ID, cd.ID, cd.OK, cd.Error)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		if c != nil {
			a.Hub.Publish("command.updated", c.Event())
		}
	}

	if err := a.Health.OnHeartbeat(ctx, d, raw); err != nil {
		slog.Error("health on heartbeat", "device", d.ID, "err", err)
	}

	pending, newlySent, err := store.PendingCommands(ctx, a.DB, d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	for _, c := range newlySent {
		a.Hub.Publish("command.updated", c.Event())
	}
	cmds := make([]commandOut, 0, len(pending))
	for _, c := range pending {
		cmds = append(cmds, commandOut{ID: c.ID, Type: c.Type, Params: c.Params, ExpiresAt: c.ExpiresAt})
	}

	cur, err := store.GetDevice(ctx, a.DB, d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	var cfg *deviceConfig
	if req.ConfigVersion < cur.ConfigVersion {
		vals, err := a.Settings.Effective(ctx, cur.Settings)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		cfg = &deviceConfig{
			ConfigVersion: cur.ConfigVersion, Mode: cur.Mode, LiveUntil: cur.LiveUntil,
			Zones: cur.Zones, Settings: vals.DeviceScoped(),
		}
	}
	extra, err := a.takeExtraFrames(ctx, d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.publishStatus(ctx, d.ID)
	httpx.OK(w, map[string]any{
		"server_time":  time.Now().UTC(),
		"commands":     cmds,
		"config":       cfg,
		"extra_frames": extra,
	})
}

// takeExtraFrames отдаёт запрошенные сервером доп. кадры и обнуляет счётчик.
func (a *API) takeExtraFrames(ctx context.Context, deviceID string) (int, error) {
	var n int
	err := a.DB.QueryRow(ctx, `UPDATE devices d SET extra_frames_requested=0
		FROM (SELECT extra_frames_requested FROM devices WHERE id=$1 FOR UPDATE) old
		WHERE d.id=$1 RETURNING old.extra_frames_requested`, deviceID).Scan(&n)
	return n, err
}

type frameMeta struct {
	FrameID       string          `json:"frame_id"`
	Kind          string          `json:"kind"`
	TakenAt       time.Time       `json:"taken_at"`
	Rotation      int             `json:"rotation"`
	CropRect      json.RawMessage `json:"crop_rect"`
	Zoom          *float64        `json:"zoom"`
	ConfigVersion *int            `json:"config_version"`
	Diff          *float64        `json:"diff"`
	CommandID     *string         `json:"command_id"`
}

func (a *API) frames(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := deviceFrom(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxFrameBody)
	if err := r.ParseMultipartForm(maxFrameBody); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var m frameMeta
	if err := json.Unmarshal([]byte(r.FormValue("meta")), &m); err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "meta: "+err.Error())
		return
	}
	if !store.ValidFrameID(m.FrameID) || !slices.Contains(frameKinds, m.Kind) || m.TakenAt.IsZero() ||
		!slices.Contains([]int{0, 90, 180, 270}, m.Rotation) {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "invalid meta")
		return
	}

	var exists bool
	if err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM frames WHERE id=$1)`, m.FrameID).Scan(&exists); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if exists {
		httpx.OK(w, map[string]any{"ok": true, "extra_frames": 0})
		return
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "image: "+err.Error())
		return
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	saved, err := store.SaveFrame(a.FramesDir, d.ID, m.FrameID, m.TakenAt, data, m.Rotation)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_image", err.Error())
		return
	}

	vals, err := a.Settings.Effective(ctx, d.Settings)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	var crop []byte
	if len(m.CropRect) > 0 && string(m.CropRect) != "null" {
		crop = m.CropRect
	}
	status, err := a.frameStatus(ctx, d, m.Kind, vals)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	tag, err := a.DB.Exec(ctx, `INSERT INTO frames (id, device_id, kind, taken_at, path, thumb_path, width, height,
		crop_rect, zoom, config_version, diff_score, command_id, status, keep_until)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10,$11,$12,$13,$14,$15) ON CONFLICT (id) DO NOTHING`,
		m.FrameID, d.ID, m.Kind, m.TakenAt, saved.Path, saved.ThumbPath, saved.Width, saved.Height,
		crop, m.Zoom, m.ConfigVersion, m.Diff, m.CommandID, status, time.Now().Add(vals.Days("storage.frame_days")))
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 { // гонка с параллельной отправкой того же кадра
		httpx.OK(w, map[string]any{"ok": true, "extra_frames": 0})
		return
	}
	if _, err := a.DB.Exec(ctx, `UPDATE devices SET last_frame_id=$2, last_frame_at=$3
		WHERE id=$1 AND (last_frame_at IS NULL OR last_frame_at <= $3)`, d.ID, m.FrameID, m.TakenAt); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if m.Kind == "reference" {
		if _, err := a.DB.Exec(ctx, `UPDATE devices SET reference_frame_id=$2 WHERE id=$1 AND reference_frame_id IS NULL`,
			d.ID, m.FrameID); err != nil {
			httpx.Internal(w, r, err)
			return
		}
	}
	a.Hub.Publish("frame.created", map[string]any{"device_id": d.ID, "frame_id": m.FrameID, "kind": m.Kind, "taken_at": m.TakenAt})
	if a.OnFrame != nil {
		a.OnFrame(context.WithoutCancel(ctx), d.ID, m.FrameID, m.Kind, m.CommandID)
	}
	a.publishStatus(ctx, d.ID)

	extra, err := a.takeExtraFrames(ctx, d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"ok": true, "extra_frames": extra})
}

// frameStatus — нужно ли отправлять кадр в VLM (docs/05-backend.md §3): только в режиме armed;
// кадры изменений — всегда, плановые — если vision.classify_keyframes_idle или идёт визит.
func (a *API) frameStatus(ctx context.Context, d *store.Device, kind string, vals settings.Values) (string, error) {
	if d.Mode != "armed" {
		return "skipped", nil
	}
	switch kind {
	case "change":
		return "pending", nil
	case "keyframe":
		if vals.Bool("vision.classify_keyframes_idle") {
			return "pending", nil
		}
		var open bool
		err := a.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM visits WHERE device_id=$1 AND state='PRESENT')`, d.ID).Scan(&open)
		if open {
			return "pending", err
		}
		return "skipped", err
	}
	return "skipped", nil
}

func (a *API) events(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := deviceFrom(r)
	var req struct {
		Events []struct {
			ID   string          `json:"id"`
			Type string          `json:"type"`
			Time time.Time       `json:"time"`
			Data json.RawMessage `json:"data"`
		} `json:"events"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	for _, e := range req.Events {
		if !store.ValidFrameID(e.ID) || e.Type == "" || len(e.Type) > 32 || e.Time.IsZero() {
			httpx.Error(w, http.StatusBadRequest, "bad_request", "invalid event")
			return
		}
		var data []byte
		if len(e.Data) > 0 {
			data = e.Data
		}
		tag, err := a.DB.Exec(ctx, `INSERT INTO events (id, device_id, type, happened_at, payload)
			VALUES ($1, $2, $3, $4, $5::jsonb) ON CONFLICT (id) DO NOTHING`, e.ID, d.ID, e.Type, e.Time, data)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		// Повтор из очереди телефона (тот же id) проблему второй раз не открывает.
		if tag.RowsAffected() == 1 {
			if err := a.Health.OnEvent(ctx, d, e.Type, e.Data); err != nil {
				slog.Error("health on event", "device", d.ID, "event", e.Type, "err", err)
			}
			a.Hub.Publish("event.created", map[string]any{"device_id": d.ID, "id": e.ID, "type": e.Type, "time": e.Time})
		}
	}
	httpx.OK(w, map[string]any{"ok": true})
}

func (a *API) publishStatus(ctx context.Context, deviceID string) {
	sum, err := store.DeviceSummary(ctx, a.DB, deviceID)
	if err != nil {
		slog.Error("device summary", "device", deviceID, "err", err)
		return
	}
	a.Hub.Publish("device.status", sum)
}
