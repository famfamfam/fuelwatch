// Package health — проблемы здоровья устройств (docs/05-backend.md §6). Правила — rules.go, здесь — БД и уведомления.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelwatch/internal/notify"
	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
	"fuelwatch/internal/stream"
)

const checkInterval = 10 * time.Second

// Названия проблем для текстов уведомлений.
var labels = map[string]string{
	notify.Offline:        "нет связи",
	notify.PowerOff:       "отключено питание",
	notify.LowBattery:     "низкий заряд",
	notify.Overheat:       "перегрев",
	notify.CameraStale:    "камера не присылает кадры",
	notify.Moved:          "телефон сдвинули",
	notify.ViewChanged:    "вид камеры изменился",
	notify.ViewBlocked:    "вид камеры закрыт",
	notify.TheftSuspected: "возможная кража телефона",
}

type Service struct {
	DB       *pgxpool.Pool
	Settings *settings.Service
	Notify   *notify.Service
	Hub      *stream.Hub
}

func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(checkInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.checkOffline(ctx); err != nil && ctx.Err() == nil {
				slog.Error("health: offline check", "err", err)
			}
			if err := s.expireCommands(ctx); err != nil && ctx.Err() == nil {
				slog.Error("health: expire commands", "err", err)
			}
		}
	}
}

func (s *Service) checkOffline(ctx context.Context) error {
	global, err := s.Settings.Global(ctx)
	if err != nil {
		return err
	}
	rows, err := s.DB.Query(ctx, `SELECT id, name, last_seen_at, settings FROM devices d
		WHERE disabled_at IS NULL AND last_seen_at IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM health_issues h WHERE h.device_id=d.id AND h.type='OFFLINE' AND h.closed_at IS NULL)`)
	if err != nil {
		return err
	}
	type cand struct {
		id, name string
		lastSeen time.Time
	}
	var stale []cand
	for rows.Next() {
		var c cand
		var overrides map[string]any
		if err := rows.Scan(&c.id, &c.name, &c.lastSeen, &overrides); err != nil {
			rows.Close()
			return err
		}
		vals, _ := settings.Resolve(global, overrides)
		if time.Since(c.lastSeen) > vals.Seconds("health.offline_after_s") {
			stale = append(stale, c)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, c := range stale {
		title := fmt.Sprintf("🔴 %s: нет связи с %s", c.name, c.lastSeen.Local().Format("15:04"))
		if _, err := s.open(ctx, c.id, c.name, notify.Offline, title, "", map[string]any{"last_seen_at": c.lastSeen}); err != nil {
			return err
		}
	}
	return nil
}

// open открывает проблему, если такой ещё нет, уведомляет и проверяет подозрение на кражу.
func (s *Service) open(ctx context.Context, deviceID, name, typ, title, body string, details any) (bool, error) {
	var id int64
	err := s.DB.QueryRow(ctx, `INSERT INTO health_issues (device_id, type, details) VALUES ($1, $2, $3)
		ON CONFLICT (device_id, type) WHERE closed_at IS NULL DO NOTHING RETURNING id`, deviceID, typ, details).Scan(&id)
	if store.IsNoRows(err) {
		return false, nil // уже открыта
	}
	if err != nil {
		return false, err
	}
	n := notify.Notification{DeviceID: &deviceID, Type: typ, Title: title, Body: body, IssueID: &id}
	var frame *string
	s.DB.QueryRow(ctx, `SELECT last_frame_id FROM devices WHERE id=$1`, deviceID).Scan(&frame)
	if typ != notify.Offline && typ != notify.PowerOff {
		n.FrameID = frame
	}
	if _, err := s.Notify.Notify(ctx, n); err != nil {
		return true, err
	}
	s.Hub.Publish("issue.updated", map[string]any{"id": id, "device_id": deviceID, "type": typ, "open": true})
	s.publishStatus(ctx, deviceID)
	if typ == notify.Moved || typ == notify.PowerOff || typ == notify.Offline {
		if err := s.checkTheft(ctx, deviceID, name, frame); err != nil {
			return true, err
		}
	}
	return true, nil
}

// close закрывает открытую проблему и уведомляет о восстановлении.
func (s *Service) close(ctx context.Context, deviceID, deviceName, typ string, by *int64) error {
	var id int64
	var opened time.Time
	err := s.DB.QueryRow(ctx, `UPDATE health_issues SET closed_at=now(), closed_by=$3
		WHERE device_id=$1 AND type=$2 AND closed_at IS NULL RETURNING id, opened_at`, deviceID, typ, by).Scan(&id, &opened)
	if store.IsNoRows(err) {
		return nil
	}
	if err != nil {
		return err
	}
	title := fmt.Sprintf("✅ %s: %s — восстановлено (было %d мин)", deviceName, labels[typ], int(time.Since(opened).Round(time.Minute).Minutes()))
	if _, err := s.Notify.Notify(ctx, notify.Notification{DeviceID: &deviceID, Type: notify.IssueResolved, Title: title, IssueID: &id}); err != nil {
		return err
	}
	s.Hub.Publish("issue.updated", map[string]any{"id": id, "device_id": deviceID, "type": typ, "open": false})
	s.publishStatus(ctx, deviceID)
	return nil
}

// CloseManual — закрыть проблему из панели (например, THEFT_SUSPECTED после проверки).
func (s *Service) CloseManual(ctx context.Context, issueID, userID int64) (bool, error) {
	var dev, name, typ string
	err := s.DB.QueryRow(ctx, `SELECT h.device_id, d.name, h.type FROM health_issues h JOIN devices d ON d.id=h.device_id
		WHERE h.id=$1 AND h.closed_at IS NULL`, issueID).Scan(&dev, &name, &typ)
	if store.IsNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, s.close(ctx, dev, name, typ, &userID)
}

// checkTheft — открыт MOVED, и в пределах health.theft_window_s открылись POWER_OFF или OFFLINE.
func (s *Service) checkTheft(ctx context.Context, deviceID, name string, frame *string) error {
	var moved, lost *time.Time
	var overrides map[string]any
	var mode string
	err := s.DB.QueryRow(ctx, `SELECT
		(SELECT opened_at FROM health_issues WHERE device_id=$1 AND type='MOVED' AND closed_at IS NULL),
		(SELECT max(opened_at) FROM health_issues WHERE device_id=$1 AND type IN ('POWER_OFF','OFFLINE') AND closed_at IS NULL),
		settings, mode FROM devices WHERE id=$1`, deviceID).Scan(&moved, &lost, &overrides, &mode)
	if err != nil {
		return err
	}
	vals, err := s.Settings.Effective(ctx, overrides)
	if err != nil {
		return err
	}
	if mode == "paused" || !Theft(moved, lost, vals.Seconds("health.theft_window_s")) {
		return nil
	}
	body := "Телефон сдвинули, и после этого пропало питание или связь. Последний кадр — во вложении."
	_, err = s.open(ctx, deviceID, name, notify.TheftSuspected, "🚨 "+name+": возможная кража телефона", body, nil)
	return err
}

func (s *Service) apply(ctx context.Context, d *store.Device, actions map[string]Action, titles map[string][2]string) error {
	for typ, a := range actions {
		var err error
		switch a {
		case Open:
			t := titles[typ]
			if t[0] == "" {
				t[0] = fmt.Sprintf("⚠ %s: %s", d.Name, labels[typ])
			}
			_, err = s.open(ctx, d.ID, d.Name, typ, t[0], t[1], nil)
		case Close:
			err = s.close(ctx, d.ID, d.Name, typ, nil)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// OnHeartbeat — связь есть (закрыть OFFLINE); питание, заряд, перегрев, камера — по данным heartbeat.
func (s *Service) OnHeartbeat(ctx context.Context, d *store.Device, raw []byte) error {
	if err := s.close(ctx, d.ID, d.Name, notify.Offline, nil); err != nil {
		return err
	}
	var hb HB
	if err := json.Unmarshal(raw, &hb); err != nil {
		return nil
	}
	vals, err := s.Settings.Effective(ctx, d.Settings)
	if err != nil {
		return err
	}
	actions := FromHeartbeat(hb, LimitsFrom(vals), d.Mode == "paused")
	titles := map[string][2]string{}
	if hb.Battery != nil {
		titles[notify.PowerOff] = [2]string{"", fmt.Sprintf("Заряд %d%%. Проверьте адаптер и кабель.", int(*hb.Battery))}
		titles[notify.LowBattery] = [2]string{fmt.Sprintf("⚠ %s: низкий заряд %d%%", d.Name, int(*hb.Battery)), ""}
	}
	if hb.BatteryTempC != nil {
		titles[notify.Overheat] = [2]string{fmt.Sprintf("⚠ %s: перегрев %.0f °C", d.Name, *hb.BatteryTempC), ""}
	}
	if hb.LastFrameAgeS != nil {
		titles[notify.CameraStale] = [2]string{fmt.Sprintf("⚠ %s: камера не присылает кадры %d с", d.Name, int(*hb.LastFrameAgeS)),
			"Телефон перезапускает камеру сам. Если не поможет — «Перезапустить приложение»."}
	}
	return s.apply(ctx, d, actions, titles)
}

// OnEvent — события телефона: питание и сдвиг (docs/07-api.md §1, events).
func (s *Service) OnEvent(ctx context.Context, d *store.Device, typ string, data json.RawMessage) error {
	var ev struct {
		Battery *float64 `json:"battery"`
		TiltDeg *float64 `json:"tilt_deg"`
		Shake   *float64 `json:"shake"`
	}
	json.Unmarshal(data, &ev)
	switch typ {
	case "POWER_OFF":
		body := ""
		if ev.Battery != nil {
			body = fmt.Sprintf("Заряд %d%%. Проверьте адаптер и кабель.", int(*ev.Battery))
		}
		_, err := s.open(ctx, d.ID, d.Name, notify.PowerOff, fmt.Sprintf("⚠ %s: отключено питание", d.Name), body, nil)
		return err
	case "POWER_ON":
		return s.close(ctx, d.ID, d.Name, notify.PowerOff, nil)
	case "MOVED":
		if d.Mode == "paused" {
			return nil // обслуживание: трогать телефон можно
		}
		body := "Проверьте кадр. После проверки нажмите «Включить» — наклон запомнится заново."
		switch {
		case ev.TiltDeg != nil:
			body = fmt.Sprintf("Наклон %.1f°. %s", *ev.TiltDeg, body)
		case ev.Shake != nil:
			body = fmt.Sprintf("Удар или тряска (%.1f м/с²). %s", *ev.Shake, body)
		}
		_, err := s.open(ctx, d.ID, d.Name, notify.Moved, fmt.Sprintf("⚠ %s: телефон сдвинули", d.Name), body, map[string]any{"event": ev})
		return err
	}
	return nil
}

// OnArm — оператор проверил кадр и включил мониторинг: MOVED закрывается.
func (s *Service) OnArm(ctx context.Context, deviceID, name string, userID int64) error {
	return s.close(ctx, deviceID, name, notify.Moved, &userID)
}

// OnReference — новый эталон: прежний «вид изменился» больше не актуален.
func (s *Service) OnReference(ctx context.Context, deviceID, name string, userID int64) error {
	if _, err := s.DB.Exec(ctx, `UPDATE devices SET view_streak='{}' WHERE id=$1`, deviceID); err != nil {
		return err
	}
	return s.close(ctx, deviceID, name, notify.ViewChanged, &userID)
}

// OnObservation — вид камеры по ответу VLM.
func (s *Service) OnObservation(ctx context.Context, deviceID string, viewMatches, obstructed bool) error {
	d, err := store.GetDevice(ctx, s.DB, deviceID)
	if err != nil {
		return err
	}
	var st Streak
	if err := s.DB.QueryRow(ctx, `SELECT view_streak FROM devices WHERE id=$1`, deviceID).Scan(&st); err != nil {
		return err
	}
	vals, err := s.Settings.Effective(ctx, d.Settings)
	if err != nil {
		return err
	}
	st, actions := FromObservation(st, viewMatches, obstructed, vals.Int("health.view_bad_n"), d.Mode == "paused")
	if _, err := s.DB.Exec(ctx, `UPDATE devices SET view_streak=$2 WHERE id=$1`, deviceID, st); err != nil {
		return err
	}
	titles := map[string][2]string{
		notify.ViewChanged: {"", "Камера смотрит не туда, куда при эталоне. Проверьте кадр; если вид верный — сделайте новый эталон."},
		notify.ViewBlocked: {"", "Объектив закрыт или запотел — проверьте на месте."},
	}
	return s.apply(ctx, d, actions, titles)
}

func (s *Service) publishStatus(ctx context.Context, deviceID string) {
	sum, err := store.DeviceSummary(ctx, s.DB, deviceID)
	if err != nil {
		slog.Error("health: device summary", "device", deviceID, "err", err)
		return
	}
	s.Hub.Publish("device.status", sum)
}

func (s *Service) expireCommands(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `UPDATE commands SET done_at=now(), ok=false, error='expired'
		WHERE done_at IS NULL AND expires_at < now() RETURNING id, device_id, type`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, dev, typ string
		if err := rows.Scan(&id, &dev, &typ); err != nil {
			return err
		}
		s.Hub.Publish("command.updated", map[string]any{"id": id, "device_id": dev, "type": typ, "status": "expired", "error": "expired"})
	}
	return rows.Err()
}
