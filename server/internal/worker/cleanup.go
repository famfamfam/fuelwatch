// Package worker — фоновые задачи. Шаг 1: очистка (docs/05-backend.md §9).
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
)

const (
	cleanupInterval = time.Hour
	cleanupBatch    = 1000
)

type Cleanup struct {
	DB        *pgxpool.Pool
	Settings  *settings.Service
	FramesDir string
}

func (c *Cleanup) Run(ctx context.Context) {
	c.once(ctx)
	t := time.NewTicker(cleanupInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.once(ctx)
		}
	}
}

func (c *Cleanup) once(ctx context.Context) {
	if err := c.frames(ctx); err != nil && ctx.Err() == nil {
		slog.Error("cleanup frames", "err", err)
	}
	vals, err := c.Settings.Defaults(ctx)
	if err != nil {
		slog.Error("cleanup settings", "err", err)
		return
	}
	steps := []struct {
		name string
		sql  string
		args []any
	}{
		{"heartbeats", `DELETE FROM heartbeats WHERE at < $1`, []any{time.Now().Add(-vals.Days("storage.heartbeat_days"))}},
		{"sessions", `DELETE FROM sessions WHERE expires_at < now()`, nil},
		{"pairing_codes", `DELETE FROM pairing_codes WHERE expires_at < now() - interval '1 day'`, nil},
	}
	for _, s := range steps {
		if _, err := c.DB.Exec(ctx, s.sql, s.args...); err != nil && ctx.Err() == nil {
			slog.Error("cleanup", "table", s.name, "err", err)
		}
	}
}

// frames удаляет просроченные кадры порциями. Эталоны и последние кадры устройств не трогаются.
func (c *Cleanup) frames(ctx context.Context) error {
	total := 0
	for {
		rows, err := c.DB.Query(ctx, `DELETE FROM frames WHERE id IN (
			SELECT f.id FROM frames f WHERE f.keep_until < now()
			  AND NOT EXISTS (SELECT 1 FROM devices d WHERE d.reference_frame_id = f.id OR d.last_frame_id = f.id)
			  AND NOT EXISTS (SELECT 1 FROM zone_versions z WHERE z.reference_frame_id = f.id)
			LIMIT $1) RETURNING path, thumb_path`, cleanupBatch)
		if err != nil {
			return err
		}
		n := 0
		for rows.Next() {
			var p, t string
			if err := rows.Scan(&p, &t); err != nil {
				rows.Close()
				return err
			}
			store.RemoveFrameFiles(c.FramesDir, p, t)
			n++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		total += n
		if n < cleanupBatch {
			break
		}
	}
	if total > 0 {
		slog.Info("cleanup: frames removed", "count", total)
	}
	return nil
}
