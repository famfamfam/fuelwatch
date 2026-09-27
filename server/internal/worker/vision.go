package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelwatch/internal/health"
	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
	"fuelwatch/internal/stream"
	"fuelwatch/internal/vision"
	"fuelwatch/internal/vision/llm"
	"fuelwatch/internal/visits"
	"fuelwatch/internal/zones"
)

const (
	idlePoll        = 5 * time.Second
	staleProcessing = 5 * time.Minute
	retryBackoff    = 2 * time.Second
)

// Vision — фоновая обработка кадров pending → observations → визиты (docs/05-backend.md §4.5).
type Vision struct {
	DB         *pgxpool.Pool
	Classifier vision.Classifier
	Settings   *settings.Service
	Visits     *visits.Service
	Health     *health.Service
	Hub        *stream.Hub
	FramesDir  string
	wake       chan struct{}
	once       sync.Once
}

// Wake будит воркеры: пришёл новый кадр.
func (v *Vision) Wake() {
	v.init()
	select {
	case v.wake <- struct{}{}:
	default:
	}
}

func (v *Vision) init() { v.once.Do(func() { v.wake = make(chan struct{}, 1) }) }

func (v *Vision) Run(ctx context.Context) {
	v.init()
	vals, err := v.Settings.Defaults(ctx)
	n := 2
	if err == nil {
		n = vals.Int("vision.workers")
	}
	slog.Info("vision workers started", "count", n)
	for i := 0; i < n; i++ {
		go v.loop(ctx)
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		// После падения сервера «зависшие» processing возвращаются в очередь.
		if _, err := v.DB.Exec(ctx, `UPDATE frames SET status='pending' WHERE status='processing' AND processing_started_at < $1`,
			time.Now().Add(-staleProcessing)); err != nil && ctx.Err() == nil {
			slog.Error("vision: reset stale", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (v *Vision) loop(ctx context.Context) {
	for ctx.Err() == nil {
		f, err := v.claim(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("vision: claim", "err", err)
		}
		if f == nil {
			select {
			case <-ctx.Done():
				return
			case <-v.wake:
			case <-time.After(idlePoll):
			}
			continue
		}
		v.process(ctx, f)
	}
}

type claimed struct {
	id, deviceID, kind string
	takenAt            time.Time
}

// claim берёт самый старый pending-кадр. По одному кадру на устройство одновременно — визиты считаются по порядку.
func (v *Vision) claim(ctx context.Context) (*claimed, error) {
	var c claimed
	err := v.DB.QueryRow(ctx, `UPDATE frames SET status='processing', processing_started_at=now(), attempts=attempts+1
		WHERE id = (SELECT f.id FROM frames f WHERE f.status='pending'
			AND NOT EXISTS (SELECT 1 FROM frames p WHERE p.device_id=f.device_id AND p.status='processing')
			ORDER BY f.taken_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING id, device_id, kind, taken_at`).Scan(&c.id, &c.deviceID, &c.kind, &c.takenAt)
	if store.IsNoRows(err) {
		return nil, nil
	}
	return &c, err
}

func (v *Vision) process(ctx context.Context, f *claimed) {
	in, err := LoadInput(ctx, v.DB, v.FramesDir, f.id)
	var obs vision.Observation
	var meta vision.CallMeta
	if err == nil {
		obs, meta, err = v.classify(ctx, in)
	}
	obsID, saveErr := SaveObservation(ctx, v.DB, f.id, f.deviceID, obs, meta, err, false)
	if saveErr != nil {
		slog.Error("vision: save observation", "frame", f.id, "err", saveErr)
	}
	status := "done"
	if err != nil {
		status = "failed"
		slog.Warn("vision: classify failed", "frame", f.id, "err", err)
	}
	if _, e := v.DB.Exec(ctx, `UPDATE frames SET status=$2 WHERE id=$1`, f.id, status); e != nil {
		slog.Error("vision: frame status", "frame", f.id, "err", e)
	}
	v.Hub.Publish("observation.created", observationEvent(obsID, f.deviceID, f.id, obs, err, false))
	if err != nil {
		return
	}

	if v.Health != nil {
		if err := v.Health.OnObservation(ctx, f.deviceID, obs.ViewMatchesReference, obs.ViewObstructed); err != nil {
			slog.Error("health: on observation", "frame", f.id, "err", err)
		}
	}
	vals, e := v.Settings.Defaults(ctx)
	late := e == nil && time.Since(f.takenAt) > vals.Seconds("visit.late_after_s")
	if err := v.Visits.OnObservation(ctx, visits.Input{
		DeviceID: f.deviceID, FrameID: f.id, TakenAt: f.takenAt, Late: late,
		Obs: visits.Obs{TankerPresent: obs.TankerPresent, TankerInZone: obs.TankerInZone,
			Obstructed: obs.ViewObstructed, Confidence: obs.Confidence},
	}); err != nil {
		slog.Error("visits: on observation", "frame", f.id, "err", err)
	}
}

// classify — повтор при 429/5xx/таймауте до vision.max_attempts.
func (v *Vision) classify(ctx context.Context, in vision.ClassifyInput) (vision.Observation, vision.CallMeta, error) {
	vals, err := v.Settings.Defaults(ctx)
	if err != nil {
		return vision.Observation{}, vision.CallMeta{}, err
	}
	attempts := max(1, vals.Int("vision.max_attempts"))
	for i := 1; ; i++ {
		obs, meta, err := v.Classifier.Classify(ctx, in)
		if err == nil || i >= attempts || errors.Is(err, vision.ErrNotConfigured) || !llm.Retryable(err) {
			return obs, meta, err
		}
		select {
		case <-ctx.Done():
			return obs, meta, ctx.Err()
		case <-time.After(time.Duration(i) * retryBackoff):
		}
	}
}

func observationEvent(id int64, deviceID, frameID string, o vision.Observation, err error, dry bool) map[string]any {
	ev := map[string]any{"id": id, "device_id": deviceID, "frame_id": frameID, "dry_run": dry}
	if err != nil {
		ev["error"] = err.Error()
	} else {
		ev["tanker_present"], ev["confidence"] = o.TankerPresent, o.Confidence
	}
	return ev
}

// LoadInput собирает вход классификатора по кадру: изображение, вырез, эталон, зоны.
func LoadInput(ctx context.Context, db *pgxpool.Pool, framesDir, frameID string) (vision.ClassifyInput, error) {
	var in vision.ClassifyInput
	var path, kind string
	var crop []byte
	var zonesRaw []byte
	var refPath *string
	err := db.QueryRow(ctx, `SELECT f.path, f.kind, f.crop_rect, d.name, d.zones, r.path
		FROM frames f JOIN devices d ON d.id=f.device_id LEFT JOIN frames r ON r.id=d.reference_frame_id
		WHERE f.id=$1`, frameID).Scan(&path, &kind, &crop, &in.DeviceName, &zonesRaw, &refPath)
	if err != nil {
		return in, fmt.Errorf("load frame: %w", err)
	}
	if in.Frame, err = loadJPEG(framesDir, path); err != nil {
		return in, err
	}
	if len(crop) > 0 && string(crop) != "null" {
		var r vision.Rect
		if err := json.Unmarshal(crop, &r); err != nil {
			return in, fmt.Errorf("crop_rect: %w", err)
		}
		in.CropRect = &r
	}
	if refPath != nil {
		// Эталон может быть удалён вручную — тогда просто без него.
		in.Reference, _ = loadJPEG(framesDir, *refPath)
	}
	if len(zonesRaw) > 0 {
		var zs []zones.Zone
		if json.Unmarshal(zonesRaw, &zs) == nil {
			in.Zones = zs
		}
	}
	in.Keyframe = kind == "keyframe"
	return in, nil
}

func loadJPEG(dir, rel string) (image.Image, error) {
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return jpeg.Decode(f)
}

// SaveObservation записывает ответ модели (или ошибку) с учётом стоимости.
func SaveObservation(ctx context.Context, db *pgxpool.Pool, frameID, deviceID string, o vision.Observation, m vision.CallMeta, callErr error, dry bool) (int64, error) {
	var result []byte
	var tanker *bool
	var conf *float64
	var errText *string
	if callErr != nil {
		s := callErr.Error()
		errText = &s
	} else {
		result, _ = json.Marshal(o)
		tanker, conf = &o.TankerPresent, &o.Confidence
	}
	var id int64
	err := db.QueryRow(ctx, `INSERT INTO observations (frame_id, device_id, result, tanker_present, confidence, provider, model,
		prompt_version, tokens_in, tokens_out, cost, latency_ms, error, dry_run)
		VALUES ($1,$2,$3::jsonb,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id`,
		frameID, deviceID, result, tanker, conf, m.Provider, m.Model, m.PromptVersion, m.TokensIn, m.TokensOut, m.Cost,
		m.Latency.Milliseconds(), errText, dry).Scan(&id)
	return id, err
}

// ClassifyNow — проверка на кадре из панели (dry-run): результат сохраняется с dry_run=true и на визиты не влияет.
func ClassifyNow(ctx context.Context, db *pgxpool.Pool, c vision.Classifier, hub *stream.Hub, framesDir, frameID, model string) (int64, vision.Observation, vision.CallMeta, error) {
	in, err := LoadInput(ctx, db, framesDir, frameID)
	if err != nil {
		return 0, vision.Observation{}, vision.CallMeta{}, err
	}
	in.Model = model
	var deviceID string
	if err := db.QueryRow(ctx, `SELECT device_id FROM frames WHERE id=$1`, frameID).Scan(&deviceID); err != nil {
		return 0, vision.Observation{}, vision.CallMeta{}, err
	}
	obs, meta, callErr := c.Classify(ctx, in)
	id, err := SaveObservation(ctx, db, frameID, deviceID, obs, meta, callErr, true)
	if err != nil {
		return 0, obs, meta, err
	}
	hub.Publish("observation.created", observationEvent(id, deviceID, frameID, obs, callErr, true))
	return id, obs, meta, callErr
}
