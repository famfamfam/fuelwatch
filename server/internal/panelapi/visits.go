package panelapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"fuelwatch/internal/httpx"
	"fuelwatch/internal/notify"
	"fuelwatch/internal/store"
	"fuelwatch/internal/vision"
	"fuelwatch/internal/vision/llm"
	"fuelwatch/internal/worker"
)

type visitRow struct {
	ID           int64      `json:"id"`
	DeviceID     string     `json:"device_id"`
	DeviceName   string     `json:"device_name"`
	State        string     `json:"state"`
	StartedAt    time.Time  `json:"started_at"`
	ConfirmedAt  time.Time  `json:"confirmed_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	EndedAt      *time.Time `json:"ended_at"`
	EndReason    *string    `json:"end_reason"`
	FirstFrameID *string    `json:"first_frame_id"`
	LastFrameID  *string    `json:"last_frame_id"`
	Feedback     *string    `json:"feedback"`
}

const visitCols = `v.id, v.device_id, d.name, v.state, v.started_at, v.confirmed_at, v.last_seen_at, v.ended_at, v.end_reason,
	v.first_frame_id, v.last_frame_id, v.feedback`

func scanVisit(row interface{ Scan(...any) error }) (visitRow, error) {
	var v visitRow
	err := row.Scan(&v.ID, &v.DeviceID, &v.DeviceName, &v.State, &v.StartedAt, &v.ConfirmedAt, &v.LastSeenAt, &v.EndedAt,
		&v.EndReason, &v.FirstFrameID, &v.LastFrameID, &v.Feedback)
	return v, err
}

// GET /visits?device=&state=&from=&to=&cursor= — от новых к старым, cursor = id.
func (a *API) listVisits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := pageSize(r)
	var cursor int64
	if c := q.Get("cursor"); c != "" {
		cursor, _ = strconv.ParseInt(c, 10, 64)
	}
	rows, err := a.DB.Query(r.Context(), `SELECT `+visitCols+` FROM visits v JOIN devices d ON d.id=v.device_id
		WHERE ($1 = '' OR v.device_id = $1) AND ($2 = '' OR v.state = $2) AND ($3 = 0 OR v.id < $3)
		ORDER BY v.id DESC LIMIT $4`, q.Get("device"), q.Get("state"), cursor, limit+1)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []visitRow{}
	for rows.Next() {
		v, err := scanVisit(rows)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		items = append(items, v)
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		c := strconv.FormatInt(items[len(items)-1].ID, 10)
		next = &c
	}
	httpx.OK(w, map[string]any{"items": items, "next_cursor": next})
}

// GET /visits/{id} — визит, его кадры (от подтверждения окна до отъезда) и наблюдения по ним.
func (a *API) getVisit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	v, err := scanVisit(a.DB.QueryRow(ctx, `SELECT `+visitCols+` FROM visits v JOIN devices d ON d.id=v.device_id WHERE v.id=$1`, id))
	if store.IsNoRows(err) {
		httpx.Error(w, http.StatusNotFound, "not_found", "visit not found")
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	end := time.Now()
	if v.EndedAt != nil {
		end = v.EndedAt.Add(10 * time.Minute) // кадры отъезда — после последнего положительного
	}
	rows, err := a.DB.Query(ctx, `SELECT `+frameColsF+`, `+obsCols+`
		FROM frames f `+obsJoin+`
		WHERE f.device_id=$1 AND f.taken_at BETWEEN $2 AND $3 AND f.kind <> 'snapshot' ORDER BY f.taken_at LIMIT 500`,
		v.DeviceID, v.StartedAt.Add(-5*time.Minute), end)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	frames := []frameRow{}
	for rows.Next() {
		f, err := scanFrameObs(rows)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		frames = append(frames, f)
	}
	httpx.OK(w, map[string]any{"visit": v, "frames": frames})
}

func (a *API) visitFeedback(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		Value *string `json:"value"`
	}
	if !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	val := ""
	if req.Value != nil {
		val = *req.Value
	}
	if val != "" && val != "up" && val != "down" {
		httpx.FieldErrors(w, map[string]string{"value": "up, down или null"})
		return
	}
	uid := userFrom(r).ID
	ok, err := a.Visits.Feedback(r.Context(), id, val, &uid)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, http.StatusNotFound, "not_found", "visit not found")
		return
	}
	httpx.OK(w, map[string]bool{"ok": true})
}

func (a *API) closeVisit(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	ok, err := a.Visits.Close(r.Context(), id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, http.StatusConflict, "not_open", "визит уже закрыт")
		return
	}
	httpx.OK(w, map[string]bool{"ok": true})
}

// POST /frames/{id}/classify {model?} — проверка модели на кадре (dry-run, на визиты не влияет).
func (a *API) classifyFrame(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if r.ContentLength > 0 && !httpx.Decode(w, r, &req, maxJSONBody) {
		return
	}
	id, obs, meta, err := worker.ClassifyNow(r.Context(), a.DB, a.Classifier, a.Hub, a.FramesDir, chi.URLParam(r, "id"), req.Model)
	out := map[string]any{
		"observation_id": id, "provider": meta.Provider, "model": meta.Model, "prompt_version": meta.PromptVersion,
		"cost": meta.Cost, "tokens_in": meta.TokensIn, "tokens_out": meta.TokensOut, "latency_ms": meta.Latency.Milliseconds(),
	}
	if err != nil {
		if store.IsNoRows(errors.Unwrap(err)) {
			httpx.Error(w, http.StatusNotFound, "not_found", "frame not found")
			return
		}
		out["error"] = err.Error()
		status := http.StatusBadGateway
		if errors.Is(err, vision.ErrNotConfigured) {
			status = http.StatusServiceUnavailable
		}
		httpx.JSON(w, status, out)
		return
	}
	out["result"] = obs
	httpx.OK(w, out)
}

// GET /costs?from=&to= — вызовы VLM, токены и стоимость по дням и моделям; трафик из heartbeat.
func (a *API) costs(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseRange(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	type bucket struct {
		Day       string  `json:"day,omitempty"`
		Model     string  `json:"model,omitempty"`
		DeviceID  string  `json:"device_id,omitempty"`
		Calls     int     `json:"calls"`
		Errors    int     `json:"errors"`
		TokensIn  int64   `json:"tokens_in"`
		TokensOut int64   `json:"tokens_out"`
		Cost      float64 `json:"cost"`
	}
	query := func(group string) ([]bucket, error) {
		rows, err := a.DB.Query(ctx, `SELECT `+group+` AS g, count(*), count(*) FILTER (WHERE error IS NOT NULL),
			COALESCE(sum(tokens_in),0), COALESCE(sum(tokens_out),0), COALESCE(sum(cost),0)
			FROM observations WHERE created_at >= $1 AND created_at < $2 GROUP BY g ORDER BY g`, from, to)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []bucket{}
		for rows.Next() {
			var b bucket
			var g string
			if err := rows.Scan(&g, &b.Calls, &b.Errors, &b.TokensIn, &b.TokensOut, &b.Cost); err != nil {
				return nil, err
			}
			switch group {
			case "model":
				b.Model = g
			case "device_id":
				b.DeviceID = g
			default:
				b.Day = g
			}
			out = append(out, b)
		}
		return out, rows.Err()
	}
	byDay, err := query(`to_char(created_at AT TIME ZONE current_setting('TimeZone'), 'YYYY-MM-DD')`)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	byModel, err := query("model")
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	byDevice, err := query("device_id")
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"by_day": byDay, "by_model": byModel, "by_device": byDevice})
}

func (a *API) visionProviders(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, map[string]any{"providers": llm.Providers(), "prompt_versions": promptVersions()})
}

func promptVersions() []string {
	out := make([]string, 0, len(vision.Prompts))
	for k := range vision.Prompts {
		out = append(out, k)
	}
	return out
}

func (a *API) telegramStatus(w http.ResponseWriter, r *http.Request) {
	httpx.OK(w, map[string]bool{"configured": a.Notify.TelegramConfigured()})
}

func (a *API) telegramTest(w http.ResponseWriter, r *http.Request) {
	n, err := a.Notify.TestTelegram(r.Context())
	if errors.Is(err, notify.ErrTelegramOff) {
		httpx.Error(w, http.StatusConflict, "telegram_off", err.Error())
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "telegram_error", err.Error())
		return
	}
	httpx.OK(w, map[string]int{"sent": n})
}
