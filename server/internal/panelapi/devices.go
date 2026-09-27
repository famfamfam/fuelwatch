package panelapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"fuelwatch/internal/httpx"
	"fuelwatch/internal/store"
)

const (
	pairingCodeTTL      = time.Hour
	pairingAlphabet     = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // без похожих символов (0/O, 1/I/L)
	maxDeviceName       = 64
	commandHistoryLimit = 50
	eventHistoryLimit   = 100
)

// Команды, которые панель может отправить напрямую. arm/pause — через /mode, live — через /live (шаг 2).
var panelCommands = []string{"snapshot", "capture_reference", "restart_camera", "restart_app"}

func (a *API) publishStatus(ctx context.Context, id string) {
	sum, err := store.DeviceSummary(ctx, a.DB, id)
	if err != nil {
		slog.Error("device summary", "device", id, "err", err)
		return
	}
	a.Hub.Publish("device.status", sum)
}

// loadDevice отвечает 404 сам и возвращает nil, если устройства нет.
func (a *API) loadDevice(w http.ResponseWriter, r *http.Request) *store.Device {
	d, err := store.GetDevice(r.Context(), a.DB, chi.URLParam(r, "id"))
	if err == store.ErrNotFound {
		httpx.Error(w, http.StatusNotFound, "not_found", "device not found")
		return nil
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return nil
	}
	return d
}

func (a *API) listDevices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	devs, err := store.ListDevices(ctx, a.DB)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	issues, err := store.OpenIssues(ctx, a.DB, "")
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	visits, err := store.OpenVisits(ctx, a.DB, "")
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	out := make([]store.Summary, 0, len(devs))
	for _, d := range devs {
		s := d.Summary(issues)
		s.Visit = visits[d.ID]
		out = append(out, s)
	}
	httpx.OK(w, out)
}

func newPairingCode() string {
	b := make([]byte, 8)
	rand.Read(b)
	for i := range b {
		b[i] = pairingAlphabet[int(b[i])%len(pairingAlphabet)]
	}
	return string(b[:4]) + "-" + string(b[4:])
}

func (a *API) issuePairingCode(ctx context.Context, deviceID string) (string, time.Time, error) {
	code := newPairingCode()
	exp := time.Now().Add(pairingCodeTTL)
	if _, err := a.DB.Exec(ctx, `DELETE FROM pairing_codes WHERE device_id=$1 AND used_at IS NULL`, deviceID); err != nil {
		return "", exp, err
	}
	norm := strings.ReplaceAll(code, "-", "")
	_, err := a.DB.Exec(ctx, `INSERT INTO pairing_codes (code_hash, device_id, expires_at) VALUES ($1, $2, $3)`,
		store.HashToken(norm), deviceID, exp)
	return code, exp, err
}

func (a *API) createDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > maxDeviceName {
		httpx.FieldErrors(w, map[string]string{"name": "от 1 до 64 символов"})
		return
	}
	ctx := r.Context()
	var id string
	for n := 1; ; n++ {
		cand := fmt.Sprintf("cam-%02d", n)
		tag, err := a.DB.Exec(ctx, `INSERT INTO devices (id, name) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING`, cand, name)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		if tag.RowsAffected() == 1 {
			id = cand
			break
		}
	}
	code, exp, err := a.issuePairingCode(ctx, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	sum, err := store.DeviceSummary(ctx, a.DB, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.Hub.Publish("device.status", sum)
	httpx.JSON(w, http.StatusCreated, map[string]any{"device": sum, "pairing_code": code, "expires_at": exp})
}

func (a *API) newPairingCode(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	code, exp, err := a.issuePairingCode(r.Context(), d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"pairing_code": code, "expires_at": exp})
}

func (a *API) getDevice(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	issues, err := store.OpenIssues(ctx, a.DB, d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if issues == nil {
		issues = []store.Issue{}
	}
	sum := d.Summary(issues)
	vs, err := store.OpenVisits(ctx, a.DB, d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	sum.Visit = vs[d.ID]
	httpx.OK(w, map[string]any{
		"device":             sum,
		"last_heartbeat":     json.RawMessage(orNull(d.LastHeartbeat)),
		"camera_caps":        json.RawMessage(orNull(d.CameraCaps)),
		"issues":             issues,
		"reference_frame_id": d.ReferenceFrameID,
		"zones_version":      d.ZonesVersion,
		"created_at":         d.CreatedAt,
	})
}

func orNull(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}

func (a *API) patchDevice(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	var req struct {
		Name     *string `json:"name"`
		Disabled *bool   `json:"disabled"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	ctx := r.Context()
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len([]rune(name)) > maxDeviceName {
			httpx.FieldErrors(w, map[string]string{"name": "от 1 до 64 символов"})
			return
		}
		if _, err := a.DB.Exec(ctx, `UPDATE devices SET name=$2 WHERE id=$1`, d.ID, name); err != nil {
			httpx.Internal(w, r, err)
			return
		}
	}
	if req.Disabled != nil {
		var err error
		if *req.Disabled {
			_, err = a.DB.Exec(ctx, `UPDATE devices SET disabled_at=COALESCE(disabled_at, now()) WHERE id=$1`, d.ID)
		} else {
			_, err = a.DB.Exec(ctx, `UPDATE devices SET disabled_at=NULL WHERE id=$1`, d.ID)
		}
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
	}
	sum, err := store.DeviceSummary(ctx, a.DB, d.ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.Hub.Publish("device.status", sum)
	httpx.OK(w, sum)
}

func (a *API) commandTTL(ctx context.Context) (time.Duration, error) {
	vals, err := a.Settings.Defaults(ctx)
	if err != nil {
		return 0, err
	}
	return vals.Seconds("net.command_ttl_s"), nil
}

func (a *API) sendCommand(ctx context.Context, deviceID, typ string, params any, userID int64) (*store.Command, error) {
	ttl, err := a.commandTTL(ctx)
	if err != nil {
		return nil, err
	}
	c, err := store.CreateCommand(ctx, a.DB, deviceID, typ, params, &userID, ttl)
	if err != nil {
		return nil, err
	}
	a.Hub.Publish("command.updated", c.Event())
	return c, nil
}

func (a *API) setMode(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	cmd := map[string]string{"armed": "arm", "paused": "pause"}[req.Mode]
	if cmd == "" {
		httpx.FieldErrors(w, map[string]string{"mode": "armed или paused"})
		return
	}
	ctx := r.Context()
	if _, err := a.DB.Exec(ctx, `UPDATE devices SET mode=$2, config_version=config_version+1 WHERE id=$1`, d.ID, req.Mode); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	c, err := a.sendCommand(ctx, d.ID, cmd, nil, userFrom(r).ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if req.Mode == "armed" {
		// Оператор проверил кадр и включил мониторинг: «сдвинули» закрывается, телефон запомнит наклон заново.
		if err := a.Health.OnArm(ctx, d.ID, d.Name, userFrom(r).ID); err != nil {
			httpx.Internal(w, r, err)
			return
		}
	}
	a.publishStatus(ctx, d.ID)
	httpx.OK(w, c)
}

func (a *API) createCommand(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	var req struct {
		Type   string          `json:"type"`
		Params json.RawMessage `json:"params"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	if !slices.Contains(panelCommands, req.Type) {
		httpx.FieldErrors(w, map[string]string{"type": "допустимо: " + strings.Join(panelCommands, ", ")})
		return
	}
	var params any
	if len(req.Params) > 0 && string(req.Params) != "null" {
		params = req.Params
	}
	c, err := a.sendCommand(r.Context(), d.ID, req.Type, params, userFrom(r).ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, c)
}

func (a *API) listCommands(w http.ResponseWriter, r *http.Request) {
	cmds, err := store.ListCommands(r.Context(), a.DB, chi.URLParam(r, "id"), commandHistoryLimit)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"items": cmds})
}

func (a *API) listEvents(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `SELECT id, type, happened_at, received_at, COALESCE(payload, 'null'::jsonb)
		FROM events WHERE device_id=$1 ORDER BY happened_at DESC LIMIT $2`, chi.URLParam(r, "id"), eventHistoryLimit)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	type event struct {
		ID         string          `json:"id"`
		Type       string          `json:"type"`
		Time       time.Time       `json:"time"`
		ReceivedAt time.Time       `json:"received_at"`
		Data       json.RawMessage `json:"data"`
	}
	items := []event{}
	for rows.Next() {
		var e event
		var data []byte
		if err := rows.Scan(&e.ID, &e.Type, &e.Time, &e.ReceivedAt, &data); err != nil {
			httpx.Internal(w, r, err)
			return
		}
		e.Data = data
		items = append(items, e)
	}
	httpx.OK(w, map[string]any{"items": items})
}

// DELETE /devices/{id} — устройство, его кадры (файлы тоже), визиты, журнал. Телефон с этим токеном
// больше не подключится (401 → экран привязки). Необратимо.
func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	// id выдаёт сервер (cam-NN), но папку удаляем только для безопасного имени.
	if d.ID == "" || d.ID != filepath.Base(d.ID) || strings.ContainsAny(d.ID, `./\`) {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "bad device id")
		return
	}
	if _, err := a.DB.Exec(r.Context(), `DELETE FROM devices WHERE id=$1`, d.ID); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if err := os.RemoveAll(filepath.Join(a.FramesDir, d.ID)); err != nil {
		slog.Error("delete device frames", "device", d.ID, "err", err)
	}
	slog.Info("device deleted", "device", d.ID, "name", d.Name, "by", userFrom(r).Login)
	a.Hub.Publish("device.deleted", map[string]any{"device_id": d.ID})
	httpx.OK(w, map[string]bool{"ok": true})
}
