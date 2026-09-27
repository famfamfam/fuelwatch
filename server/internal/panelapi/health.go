package panelapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"fuelwatch/internal/httpx"
	"fuelwatch/internal/notify"
	"fuelwatch/internal/store"
)

// Шаги агрегации временных рядов heartbeat.
var seriesSteps = map[string]time.Duration{"1m": time.Minute, "10m": 10 * time.Minute, "1h": time.Hour}

type seriesPoint struct {
	T             time.Time `json:"t"`
	Battery       *float64  `json:"battery"`
	Charging      *float64  `json:"charging"` // доля heartbeat с питанием, 0..1
	BatteryTempC  *float64  `json:"battery_temp_c"`
	ThermalStatus *int      `json:"thermal_status"`
	TxBytes       *int64    `json:"tx_bytes_today"` // максимум за интервал (счётчик с начала суток)
	QueueItems    *float64  `json:"queue_items"`
	Count         int       `json:"count"`
}

// GET /devices/{id}/heartbeats?from=&to=&step=1m|10m|1h — для графиков заряда, питания, температуры, трафика.
func (a *API) heartbeatSeries(w http.ResponseWriter, r *http.Request) {
	from, to, ok := parseRange(w, r)
	if !ok {
		return
	}
	step, ok := seriesSteps[r.URL.Query().Get("step")]
	if !ok {
		step = 10 * time.Minute
	}
	rows, err := a.DB.Query(r.Context(), `SELECT date_bin($4::interval, at, $2) AS t,
			avg((body->>'battery')::float8),
			avg(CASE WHEN (body->>'charging')::boolean THEN 1.0 ELSE 0.0 END) FILTER (WHERE body ? 'charging'),
			avg((body->>'battery_temp_c')::float8),
			max((body->>'thermal_status')::int),
			max((body->>'tx_bytes_today')::bigint),
			avg((body->>'queue_items')::float8),
			count(*)
		FROM heartbeats WHERE device_id=$1 AND at >= $2 AND at < $3 GROUP BY t ORDER BY t`,
		chi.URLParam(r, "id"), from, to, step)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	out := []seriesPoint{}
	for rows.Next() {
		var p seriesPoint
		if err := rows.Scan(&p.T, &p.Battery, &p.Charging, &p.BatteryTempC, &p.ThermalStatus, &p.TxBytes, &p.QueueItems, &p.Count); err != nil {
			httpx.Internal(w, r, err)
			return
		}
		out = append(out, p)
	}
	httpx.OK(w, map[string]any{"step_s": int(step.Seconds()), "points": out})
}

func (a *API) listIssues(w http.ResponseWriter, r *http.Request) {
	rows, err := a.DB.Query(r.Context(), `SELECT id, device_id, type, opened_at, closed_at, COALESCE(details, 'null'::jsonb)
		FROM health_issues WHERE device_id=$1 AND ($2 = '' OR ($2 = 'true') = (closed_at IS NULL))
		ORDER BY opened_at DESC LIMIT 200`, chi.URLParam(r, "id"), r.URL.Query().Get("open"))
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []store.Issue{}
	for rows.Next() {
		var i store.Issue
		var details []byte
		if err := rows.Scan(&i.ID, &i.DeviceID, &i.Type, &i.OpenedAt, &i.ClosedAt, &details); err != nil {
			httpx.Internal(w, r, err)
			return
		}
		i.Details = details
		items = append(items, i)
	}
	httpx.OK(w, map[string]any{"items": items})
}

func (a *API) closeIssue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "bad id")
		return
	}
	ok, err := a.Health.CloseManual(r.Context(), id, userFrom(r).ID)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	if !ok {
		httpx.Error(w, http.StatusConflict, "not_open", "проблема уже закрыта")
		return
	}
	httpx.OK(w, map[string]bool{"ok": true})
}

type logEntry struct {
	Kind  string          `json:"kind"` // notification | command | event
	Time  time.Time       `json:"time"`
	Type  string          `json:"type"`
	Title string          `json:"title"`
	Who   *string         `json:"who,omitempty"`
	State string          `json:"state,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// GET /devices/{id}/log?kind=&limit= — единый журнал устройства (docs/06-panel.md §4.2).
func (a *API) deviceLog(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	kind := r.URL.Query().Get("kind")
	limit := pageSize(r)
	var out []logEntry
	if kind == "" || kind == "notification" {
		rows, err := a.DB.Query(ctx, `SELECT `+notify.Cols+` FROM notifications WHERE device_id=$1 ORDER BY id DESC LIMIT $2`, id, limit)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		for rows.Next() {
			n, err := notify.Scan(rows)
			if err != nil {
				rows.Close()
				httpx.Internal(w, r, err)
				return
			}
			out = append(out, logEntry{Kind: "notification", Time: n.CreatedAt, Type: n.Type, Title: n.Title, State: n.Severity})
		}
		rows.Close()
	}
	if kind == "" || kind == "command" {
		cmds, err := store.ListCommands(ctx, a.DB, id, limit)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		for _, c := range cmds {
			e := logEntry{Kind: "command", Time: c.CreatedAt, Type: c.Type, Who: c.CreatedBy, State: c.Status}
			if c.Error != nil {
				e.Title = *c.Error
			}
			out = append(out, e)
		}
	}
	if kind == "" || kind == "event" {
		rows, err := a.DB.Query(ctx, `SELECT type, happened_at, COALESCE(payload, 'null'::jsonb) FROM events
			WHERE device_id=$1 ORDER BY happened_at DESC LIMIT $2`, id, limit)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		for rows.Next() {
			var e logEntry
			var data []byte
			if err := rows.Scan(&e.Type, &e.Time, &data); err != nil {
				rows.Close()
				httpx.Internal(w, r, err)
				return
			}
			e.Kind, e.Data = "event", data
			out = append(out, e)
		}
		rows.Close()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []logEntry{}
	}
	httpx.OK(w, map[string]any{"items": out})
}
