package panelapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"fuelwatch/internal/httpx"
	"fuelwatch/internal/store"
)

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// obsBrief — последнее наблюдение (не dry-run) по кадру: для бейджей в галерее.
type obsBrief struct {
	TankerPresent *bool    `json:"tanker_present"`
	TankerInZone  *bool    `json:"tanker_in_zone"`
	Confidence    *float32 `json:"confidence"`
	Note          *string  `json:"note"`
	Error         *string  `json:"error"`
	Model         string   `json:"model"`
}

type frameRow struct {
	ID            string          `json:"id"`
	DeviceID      string          `json:"device_id"`
	Kind          string          `json:"kind"`
	TakenAt       time.Time       `json:"taken_at"`
	ReceivedAt    time.Time       `json:"received_at"`
	Width         int             `json:"width"`
	Height        int             `json:"height"`
	CropRect      json.RawMessage `json:"crop_rect"`
	Zoom          *float32        `json:"zoom"`
	ConfigVersion *int            `json:"config_version"`
	Diff          *float32        `json:"diff"`
	CommandID     *string         `json:"command_id"`
	Status        string          `json:"status"`
	Observation   *obsBrief       `json:"observation"`
}

const frameColsF = `f.id, f.device_id, f.kind, f.taken_at, f.received_at, f.width, f.height, COALESCE(f.crop_rect, 'null'::jsonb),
	f.zoom, f.config_version, f.diff_score, f.command_id, f.status`

const obsCols = `ob.id, ob.tanker_present, (ob.result->>'tanker_in_zone')::boolean, ob.confidence, ob.result->>'note', ob.error, ob.model`

const obsJoin = `LEFT JOIN LATERAL (SELECT id, tanker_present, confidence, result, error, model FROM observations o
	WHERE o.frame_id=f.id AND NOT o.dry_run ORDER BY o.id DESC LIMIT 1) ob ON true`

func scanFrameObs(row interface{ Scan(...any) error }) (frameRow, error) {
	var f frameRow
	var crop []byte
	var obsID *int64
	var o obsBrief
	var model *string
	err := row.Scan(&f.ID, &f.DeviceID, &f.Kind, &f.TakenAt, &f.ReceivedAt, &f.Width, &f.Height, &crop,
		&f.Zoom, &f.ConfigVersion, &f.Diff, &f.CommandID, &f.Status,
		&obsID, &o.TankerPresent, &o.TankerInZone, &o.Confidence, &o.Note, &o.Error, &model)
	f.CropRect = crop
	if obsID != nil {
		if model != nil {
			o.Model = *model
		}
		f.Observation = &o
	}
	return f, err
}

func pageSize(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return defaultPageSize
	}
	return min(n, maxPageSize)
}

func optTime(w http.ResponseWriter, v, name string) (*time.Time, bool) {
	if v == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "bad_request", name+": нужен формат RFC 3339")
		return nil, false
	}
	return &t, true
}

// tankerFilter — фильтр галереи по ответу модели.
var tankerFilter = map[string]string{
	"":          "true",
	"yes":       "ob.tanker_present AND (ob.result->>'tanker_in_zone')::boolean",
	"no":        "ob.tanker_present = false OR NOT COALESCE((ob.result->>'tanker_in_zone')::boolean, false)",
	"unchecked": "ob.id IS NULL",
	"error":     "ob.error IS NOT NULL",
}

// GET /frames?device=&kind=&tanker=&from=&to=&cursor=&limit= — от новых к старым. cursor = "<taken_at RFC3339Nano>|<id>".
func (a *API) listFrames(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := pageSize(r)
	var curTime *time.Time
	curID := ""
	if c := q.Get("cursor"); c != "" {
		ts, id, ok := strings.Cut(c, "|")
		t, err := time.Parse(time.RFC3339Nano, ts)
		if !ok || err != nil {
			httpx.Error(w, http.StatusBadRequest, "bad_request", "bad cursor")
			return
		}
		curTime, curID = &t, id
	}
	from, ok := optTime(w, q.Get("from"), "from")
	if !ok {
		return
	}
	to, ok := optTime(w, q.Get("to"), "to")
	if !ok {
		return
	}
	tf, ok := tankerFilter[q.Get("tanker")]
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "bad_request", "tanker: yes | no | unchecked | error")
		return
	}
	rows, err := a.DB.Query(r.Context(), `SELECT `+frameColsF+`, `+obsCols+` FROM frames f `+obsJoin+`
		WHERE ($1 = '' OR f.device_id = $1) AND ($2 = '' OR f.kind = $2)
		  AND ($3::timestamptz IS NULL OR (f.taken_at, f.id) < ($3, $4))
		  AND ($6::timestamptz IS NULL OR f.taken_at >= $6) AND ($7::timestamptz IS NULL OR f.taken_at < $7)
		  AND (`+tf+`)
		ORDER BY f.taken_at DESC, f.id DESC LIMIT $5`, q.Get("device"), q.Get("kind"), curTime, curID, limit+1, from, to)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []frameRow{}
	for rows.Next() {
		f, err := scanFrameObs(rows)
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		items = append(items, f)
	}
	if err := rows.Err(); err != nil {
		httpx.Internal(w, r, err)
		return
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		c := last.TakenAt.Format(time.RFC3339Nano) + "|" + last.ID
		next = &c
	}
	httpx.OK(w, map[string]any{"items": items, "next_cursor": next})
}

type observationRow struct {
	ID            int64           `json:"id"`
	Result        json.RawMessage `json:"result"`
	Provider      string          `json:"provider"`
	Model         string          `json:"model"`
	PromptVersion string          `json:"prompt_version"`
	TokensIn      int             `json:"tokens_in"`
	TokensOut     int             `json:"tokens_out"`
	Cost          float64         `json:"cost"`
	LatencyMS     int             `json:"latency_ms"`
	Error         *string         `json:"error"`
	DryRun        bool            `json:"dry_run"`
	CreatedAt     time.Time       `json:"created_at"`
}

// GET /frames/{id} — метаданные и все наблюдения (включая проверки из панели).
func (a *API) getFrame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	f, err := scanFrameObs(a.DB.QueryRow(ctx, `SELECT `+frameColsF+`, `+obsCols+` FROM frames f `+obsJoin+` WHERE f.id=$1`, id))
	if store.IsNoRows(err) {
		httpx.Error(w, http.StatusNotFound, "not_found", "frame not found")
		return
	}
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	rows, err := a.DB.Query(ctx, `SELECT id, COALESCE(result, 'null'::jsonb), provider, model, prompt_version, tokens_in, tokens_out,
		cost, latency_ms, error, dry_run, created_at FROM observations WHERE frame_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		httpx.Internal(w, r, err)
		return
	}
	defer rows.Close()
	obs := []observationRow{}
	for rows.Next() {
		var o observationRow
		var res []byte
		if err := rows.Scan(&o.ID, &res, &o.Provider, &o.Model, &o.PromptVersion, &o.TokensIn, &o.TokensOut, &o.Cost,
			&o.LatencyMS, &o.Error, &o.DryRun, &o.CreatedAt); err != nil {
			httpx.Internal(w, r, err)
			return
		}
		o.Result = res
		obs = append(obs, o)
	}
	httpx.OK(w, map[string]any{"frame": f, "observations": obs})
}

func (a *API) frameFile(thumb bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		col := "path"
		if thumb {
			col = "thumb_path"
		}
		var rel string
		err := a.DB.QueryRow(r.Context(), `SELECT `+col+` FROM frames WHERE id=$1`, chi.URLParam(r, "id")).Scan(&rel)
		if store.IsNoRows(err) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		f, err := os.Open(filepath.Join(a.FramesDir, filepath.FromSlash(rel)))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			httpx.Internal(w, r, err)
			return
		}
		// Файл кадра не меняется: можно кэшировать надолго.
		w.Header().Set("Cache-Control", "private, max-age=604800, immutable")
		w.Header().Set("Content-Type", "image/jpeg")
		http.ServeContent(w, r, "", st.ModTime(), f)
	}
}
