package panelapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"fuelwatch/internal/httpx"
	"fuelwatch/internal/store"
	"fuelwatch/internal/zones"
)

type zoneVersion struct {
	Version          int             `json:"version"`
	Zones            json.RawMessage `json:"zones"`
	ReferenceFrameID *string         `json:"reference_frame_id"`
	CreatedBy        *string         `json:"created_by"`
	CreatedAt        time.Time       `json:"created_at"`
}

// GET /devices/{id}/zones — текущие зоны, их версия, эталон и версия конфига (для «применено на телефоне»).
func (a *API) getZones(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	httpx.OK(w, map[string]any{
		"zones":              json.RawMessage(orNull(d.Zones)),
		"version":            d.ZonesVersion,
		"reference_frame_id": d.ReferenceFrameID,
		"config_version":     d.ConfigVersion,
	})
}

// PUT /devices/{id}/zones {zones, base_version} — новая версия. 409, если кто-то сохранил раньше.
func (a *API) putZones(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Zones       json.RawMessage `json:"zones"`
		BaseVersion int             `json:"base_version"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	zs, err := zones.Parse(req.Zones)
	if err != nil {
		httpx.FieldErrors(w, map[string]string{"zones": err.Error()})
		return
	}
	a.saveZones(w, r, chi.URLParam(r, "id"), zs, &req.BaseVersion)
}

// POST /devices/{id}/zones/restore {version} — вернуть версию (сохраняется как новая).
func (a *API) restoreZones(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version int `json:"version"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	id := chi.URLParam(r, "id")
	var raw []byte
	err := a.DB.QueryRow(r.Context(), `SELECT zones FROM zone_versions WHERE device_id=$1 AND version=$2`, id, req.Version).Scan(&raw)
	if store.IsNoRows(err) {
		httpx.Error(w, http.StatusNotFound, "not_found", "version not found")
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	zs, err := zones.Parse(raw)
	if err != nil {
		httpx.FieldErrors(w, map[string]string{"zones": err.Error()})
		return
	}
	a.saveZones(w, r, id, zs, nil)
}

func (a *API) saveZones(w http.ResponseWriter, r *http.Request, deviceID string, zs []zones.Zone, baseVersion *int) {
	ctx := r.Context()
	var version int
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var cur int
		var ref *string
		if err := tx.QueryRow(ctx, `SELECT zones_version, reference_frame_id FROM devices WHERE id=$1 FOR UPDATE`, deviceID).
			Scan(&cur, &ref); err != nil {
			return err
		}
		if baseVersion != nil && *baseVersion != cur {
			return errConflict{cur}
		}
		version = cur + 1
		if _, err := tx.Exec(ctx, `INSERT INTO zone_versions (device_id, version, zones, reference_frame_id, created_by)
			VALUES ($1, $2, $3, $4, $5)`, deviceID, version, zs, ref, userFrom(r).ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE devices SET zones=$2, zones_version=$3, config_version=config_version+1 WHERE id=$1`,
			deviceID, zs, version)
		return err
	})
	if store.IsNoRows(err) {
		httpx.Error(w, http.StatusNotFound, "not_found", "device not found")
		return
	}
	if c, ok := err.(errConflict); ok {
		httpx.JSON(w, http.StatusConflict, map[string]any{
			"error": "conflict", "message": "зоны уже изменил кто-то другой — обновите страницу", "version": c.current,
		})
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.Hub.Publish("zones.updated", map[string]any{"device_id": deviceID, "version": version})
	a.publishStatus(ctx, deviceID)
	a.getZones(w, r)
}

type errConflict struct{ current int }

func (errConflict) Error() string { return "zones version conflict" }

func (a *API) listZoneVersions(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `SELECT v.version, v.zones, v.reference_frame_id, u.login, v.created_at
		FROM zone_versions v LEFT JOIN users u ON u.id=v.created_by
		WHERE v.device_id=$1 ORDER BY v.version DESC LIMIT 100`, chi.URLParam(r, "id"))
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []zoneVersion{}
	for rows.Next() {
		var v zoneVersion
		var raw []byte
		if err := rows.Scan(&v.Version, &raw, &v.ReferenceFrameID, &v.CreatedBy, &v.CreatedAt); err != nil {
			httpx.Internal(w, r, err)
			return
		}
		v.Zones = raw
		items = append(items, v)
	}
	httpx.OK(w, map[string]any{"items": items})
}

// POST /devices/{id}/reference {frame_id} — эталоном может стать только полный кадр этого устройства.
func (a *API) setReference(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	var req struct {
		FrameID string `json:"frame_id"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	ctx := r.Context()
	var owner string
	var cropped bool
	err := a.DB.QueryRow(ctx, `SELECT device_id, crop_rect IS NOT NULL FROM frames WHERE id=$1`, req.FrameID).Scan(&owner, &cropped)
	if store.IsNoRows(err) || (err == nil && owner != d.ID) {
		httpx.FieldErrors(w, map[string]string{"frame_id": "кадр не найден у этого устройства"})
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if cropped {
		httpx.FieldErrors(w, map[string]string{"frame_id": "эталоном может быть только полный кадр, не вырез"})
		return
	}
	if _, err := a.DB.Exec(ctx, `UPDATE devices SET reference_frame_id=$2 WHERE id=$1`, d.ID, req.FrameID); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if err := a.Health.OnReference(ctx, d.ID, d.Name, userFrom(r).ID); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.publishStatus(ctx, d.ID)
	httpx.OK(w, map[string]any{"reference_frame_id": req.FrameID})
}

// POST /devices/{id}/live {duration_s} — включить или продлить живой режим; 0 — выключить.
func (a *API) setLive(w http.ResponseWriter, r *http.Request) {
	d := a.loadDevice(w, r)
	if d == nil {
		return
	}
	var req struct {
		DurationS *int `json:"duration_s"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	ctx := r.Context()
	vals, err := a.Settings.Effective(ctx, d.Settings)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	maxDur := vals.Seconds("live.duration_s")
	dur := maxDur
	if req.DurationS != nil {
		dur = min(time.Duration(max(0, *req.DurationS))*time.Second, maxDur)
	}
	var until *time.Time
	cmdUntil := time.Now()
	if dur > 0 {
		t := time.Now().Add(dur)
		until, cmdUntil = &t, t
	}
	if _, err := a.DB.Exec(ctx, `UPDATE devices SET live_until=$2, config_version=config_version+1 WHERE id=$1`, d.ID, until); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if _, err := a.sendCommand(ctx, d.ID, "live", map[string]any{"until": cmdUntil.UTC()}, userFrom(r).ID); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	a.publishStatus(ctx, d.ID)
	httpx.OK(w, map[string]any{"live_until": until})
}

// GET /devices/{id}/timeline?from=&to= — для ленты: кадры, интервалы offline, визиты (шаг 3).
func (a *API) timeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	from, to, ok := parseRange(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	type tFrame struct {
		ID      string    `json:"id"`
		TakenAt time.Time `json:"taken_at"`
		Kind    string    `json:"kind"`
	}
	type interval struct {
		From time.Time  `json:"from"`
		To   *time.Time `json:"to"`
	}
	frames := []tFrame{}
	rows, err := a.DB.Query(ctx, `SELECT id, taken_at, kind FROM frames WHERE device_id=$1 AND taken_at >= $2 AND taken_at < $3
		ORDER BY taken_at LIMIT 5000`, id, from, to)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	for rows.Next() {
		var f tFrame
		if err := rows.Scan(&f.ID, &f.TakenAt, &f.Kind); err != nil {
			rows.Close()
			httpx.Internal(w, r, err)
			return
		}
		frames = append(frames, f)
	}
	rows.Close()

	offline := []interval{}
	rows, err = a.DB.Query(ctx, `SELECT opened_at, closed_at FROM health_issues
		WHERE device_id=$1 AND type='OFFLINE' AND opened_at < $3 AND (closed_at IS NULL OR closed_at > $2) ORDER BY opened_at`,
		id, from, to)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	for rows.Next() {
		var i interval
		if err := rows.Scan(&i.From, &i.To); err != nil {
			rows.Close()
			httpx.Internal(w, r, err)
			return
		}
		offline = append(offline, i)
	}
	rows.Close()
	visits := []interval{}
	rows, err = a.DB.Query(ctx, `SELECT started_at, ended_at FROM visits
		WHERE device_id=$1 AND started_at < $3 AND (ended_at IS NULL OR ended_at > $2) ORDER BY started_at`, id, from, to)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	for rows.Next() {
		var i interval
		if err := rows.Scan(&i.From, &i.To); err != nil {
			rows.Close()
			httpx.Internal(w, r, err)
			return
		}
		visits = append(visits, i)
	}
	rows.Close()
	httpx.OK(w, map[string]any{"frames": frames, "offline": offline, "visits": visits})
}

// parseRange читает from/to (RFC 3339). По умолчанию — последние сутки.
func parseRange(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, bool) {
	to := time.Now()
	from := to.Add(-24 * time.Hour)
	q := r.URL.Query()
	for _, p := range []struct {
		name string
		dst  *time.Time
	}{{"from", &from}, {"to", &to}} {
		if v := q.Get(p.name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				httpx.Error(w, http.StatusBadRequest, "bad_request", p.name+": нужен формат RFC 3339")
				return from, to, false
			}
			*p.dst = t
		}
	}
	return from, to, true
}
