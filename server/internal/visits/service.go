package visits

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"fuelwatch/internal/notify"
	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
	"fuelwatch/internal/stream"
)

const staleCheckInterval = time.Minute

// Service хранит состояние визитов в БД и уведомляет о переходах.
type Service struct {
	DB       *pgxpool.Pool
	Notify   *notify.Service
	Hub      *stream.Hub
	Settings *settings.Service

	locks sync.Map // device_id → *sync.Mutex: наблюдения одного устройства обрабатываются по очереди
}

func (s *Service) lock(deviceID string) func() {
	m, _ := s.locks.LoadOrStore(deviceID, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Input — наблюдение по кадру устройства.
type Input struct {
	DeviceID string
	FrameID  string
	TakenAt  time.Time
	Obs      Obs
	// Late — кадр пришёл из очереди после отсутствия связи: визит считается, но без уведомлений.
	Late bool
}

type row struct {
	id int64
	Visit
}

func loadState(ctx context.Context, tx pgx.Tx, deviceID string) (State, *row, string, map[string]any, error) {
	var st State
	var name string
	var overrides map[string]any
	var pf *string
	err := tx.QueryRow(ctx, `SELECT name, settings, pending_positive_count, pending_positive_since, pending_positive_frame
		FROM devices WHERE id=$1 FOR UPDATE`, deviceID).Scan(&name, &overrides, &st.PendingCount, &st.PendingSince, &pf)
	if err != nil {
		return st, nil, "", nil, err
	}
	if pf != nil {
		st.PendingFrame = *pf
	}
	var r row
	var first, last *string
	err = tx.QueryRow(ctx, `SELECT id, started_at, last_seen_at, negatives, unloading_notified, first_frame_id, last_frame_id
		FROM visits WHERE device_id=$1 AND state='PRESENT' FOR UPDATE`, deviceID).
		Scan(&r.id, &r.StartedAt, &r.LastSeenAt, &r.Negatives, &r.UnloadingNotified, &first, &last)
	if store.IsNoRows(err) {
		return st, nil, name, overrides, nil
	}
	if err != nil {
		return st, nil, "", nil, err
	}
	if first != nil {
		r.FirstFrameID = *first
	}
	if last != nil {
		r.LastFrameID = *last
	}
	st.Open = &r.Visit
	return st, &r, name, overrides, nil
}

// OnObservation применяет наблюдение (docs/05-backend.md §5).
func (s *Service) OnObservation(ctx context.Context, in Input) error {
	defer s.lock(in.DeviceID)()

	var (
		d        Decision
		visitID  int64
		name     string
		prev     *row
		params   Params
		keepDays time.Duration
	)
	err := pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
		st, r, n, overrides, err := loadState(ctx, tx, in.DeviceID)
		if err != nil {
			return err
		}
		name, prev = n, r
		vals, err := s.Settings.Effective(ctx, overrides)
		if err != nil {
			return err
		}
		params = ParamsFrom(vals)
		keepDays = vals.Days("storage.visit_frame_days")
		in.Obs.FrameID = in.FrameID
		d = Step(st, in.Obs, in.TakenAt, params)

		ns := d.State
		if _, err := tx.Exec(ctx, `UPDATE devices SET pending_positive_count=$2, pending_positive_since=$3,
			pending_positive_frame=NULLIF($4, ''), extra_frames_requested=GREATEST(extra_frames_requested, $5) WHERE id=$1`,
			in.DeviceID, ns.PendingCount, ns.PendingSince, ns.PendingFrame, d.ExtraFrames); err != nil {
			return err
		}
		switch {
		case d.Event == Arrived:
			v := ns.Open
			return tx.QueryRow(ctx, `INSERT INTO visits (device_id, state, started_at, confirmed_at, last_seen_at, first_frame_id, last_frame_id)
				VALUES ($1, 'PRESENT', $2, $3, $4, $5, $6) RETURNING id`,
				in.DeviceID, v.StartedAt, in.TakenAt, v.LastSeenAt, nullable(v.FirstFrameID), nullable(v.LastFrameID)).Scan(&visitID)
		case d.Event == Left:
			visitID = r.id
			_, err := tx.Exec(ctx, `UPDATE visits SET state='LEFT', ended_at=$2, negatives=negatives+1, end_reason='left' WHERE id=$1`,
				r.id, d.EndedAt)
			if err != nil {
				return err
			}
			return keepVisitFrames(ctx, tx, in.DeviceID, r.StartedAt, d.EndedAt, params, keepDays)
		case ns.Open != nil:
			visitID = r.id
			v := ns.Open
			_, err := tx.Exec(ctx, `UPDATE visits SET last_seen_at=$2, last_frame_id=$3, negatives=$4, unloading_notified=$5 WHERE id=$1`,
				r.id, v.LastSeenAt, nullable(v.LastFrameID), v.Negatives, v.UnloadingNotified)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if visitID != 0 {
		s.publish(ctx, visitID)
	}
	if d.Event == "" || in.Late {
		if d.Event != "" {
			slog.Info("visit event from late frame, no notification", "device", in.DeviceID, "event", d.Event)
		}
		return nil
	}
	var n notify.Notification
	switch d.Event {
	case Arrived:
		n = notify.Notification{Type: notify.VisitArrived,
			Title: fmt.Sprintf("⛽ %s: бензовоз приехал · %s", name, hhmm(d.State.Open.StartedAt))}
	case Unloading:
		mins := minutes(in.TakenAt.Sub(d.State.Open.StartedAt))
		n = notify.Notification{Type: notify.VisitUnloading,
			Title: fmt.Sprintf("⛽ %s: бензовоз стоит %d мин — вероятно, разгрузка", name, mins)}
	case Left:
		n = notify.Notification{Type: notify.VisitLeft, Title: leftTitle(name, prev.StartedAt, d.EndedAt)}
	}
	n.DeviceID, n.FrameID, n.VisitID = &in.DeviceID, &in.FrameID, &visitID
	n.Body = s.cameraWarning(ctx, in.DeviceID)
	_, err = s.Notify.Notify(ctx, n)
	return err
}

func leftTitle(name string, from, to time.Time) string {
	return fmt.Sprintf("⛽ %s: бензовоз уехал · был %s–%s (%d мин)", name, hhmm(from), hhmm(to), minutes(to.Sub(from)))
}

func hhmm(t time.Time) string { return t.Local().Format("15:04") }

func minutes(d time.Duration) int { return int(math.Round(d.Minutes())) }

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// cameraWarning — пока открыт MOVED или VIEW_CHANGED, к уведомлениям о визитах добавляется предупреждение.
func (s *Service) cameraWarning(ctx context.Context, deviceID string) string {
	var n int
	s.DB.QueryRow(ctx, `SELECT count(*) FROM health_issues WHERE device_id=$1 AND closed_at IS NULL
		AND type IN ('MOVED', 'VIEW_CHANGED')`, deviceID).Scan(&n)
	if n > 0 {
		return "⚠ камера могла сместиться — проверьте кадр"
	}
	return ""
}

// keepVisitFrames — кадры визита хранятся дольше (storage.visit_frame_days).
func keepVisitFrames(ctx context.Context, tx pgx.Tx, deviceID string, from, to time.Time, p Params, keep time.Duration) error {
	_, err := tx.Exec(ctx, `UPDATE frames SET keep_until=GREATEST(keep_until, now() + $4::interval)
		WHERE device_id=$1 AND taken_at BETWEEN $2 AND $3`,
		deviceID, from.Add(-p.ConfirmWindow), to.Add(p.LeaveMin+time.Minute), keep)
	return err
}

type Brief struct {
	ID         int64      `json:"id"`
	DeviceID   string     `json:"device_id"`
	State      string     `json:"state"`
	StartedAt  time.Time  `json:"started_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	EndedAt    *time.Time `json:"ended_at"`
}

func (s *Service) publish(ctx context.Context, id int64) {
	var b Brief
	err := s.DB.QueryRow(ctx, `SELECT id, device_id, state, started_at, last_seen_at, ended_at FROM visits WHERE id=$1`, id).
		Scan(&b.ID, &b.DeviceID, &b.State, &b.StartedAt, &b.LastSeenAt, &b.EndedAt)
	if err != nil {
		slog.Error("visit publish", "id", id, "err", err)
		return
	}
	s.Hub.Publish("visit.updated", b)
}

// Run — предохранитель: закрыть визиты, открытые дольше visit.max_hours.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(staleCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.closeStale(ctx); err != nil && ctx.Err() == nil {
				slog.Error("visits: stale check", "err", err)
			}
		}
	}
}

func (s *Service) closeStale(ctx context.Context) error {
	rows, err := s.DB.Query(ctx, `SELECT v.id, v.device_id, d.name, d.settings, v.started_at, v.last_seen_at
		FROM visits v JOIN devices d ON d.id=v.device_id WHERE v.state='PRESENT'`)
	if err != nil {
		return err
	}
	type open struct {
		id               int64
		device, name     string
		overrides        map[string]any
		started, lastSee time.Time
	}
	var list []open
	for rows.Next() {
		var o open
		if err := rows.Scan(&o.id, &o.device, &o.name, &o.overrides, &o.started, &o.lastSee); err != nil {
			rows.Close()
			return err
		}
		list = append(list, o)
	}
	rows.Close()
	for _, o := range list {
		vals, err := s.Settings.Effective(ctx, o.overrides)
		if err != nil {
			return err
		}
		p := ParamsFrom(vals)
		if !Stale(Visit{StartedAt: o.started}, time.Now(), p) {
			continue
		}
		unlock := s.lock(o.device)
		err = pgx.BeginFunc(ctx, s.DB, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE visits SET state='LEFT', ended_at=last_seen_at, end_reason='max_hours'
				WHERE id=$1 AND state='PRESENT'`, o.id)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			return keepVisitFrames(ctx, tx, o.device, o.started, o.lastSee, p, vals.Days("storage.visit_frame_days"))
		})
		unlock()
		if err != nil {
			return err
		}
		s.publish(ctx, o.id)
		id := o.id
		dev := o.device
		if _, err := s.Notify.Notify(ctx, notify.Notification{
			DeviceID: &dev, VisitID: &id, Type: notify.VisitLeft,
			Title: leftTitle(o.name, o.started, o.lastSee),
			Body:  fmt.Sprintf("Визит был открыт дольше %d ч и закрыт автоматически — проверьте кадр", vals.Int("visit.max_hours")),
		}); err != nil {
			return err
		}
	}
	return nil
}

// Close — закрыть визит вручную (панель): без уведомления.
func (s *Service) Close(ctx context.Context, id int64) (bool, error) {
	var dev string
	err := s.DB.QueryRow(ctx, `UPDATE visits SET state='LEFT', ended_at=last_seen_at, end_reason='manual'
		WHERE id=$1 AND state='PRESENT' RETURNING device_id`, id).Scan(&dev)
	if store.IsNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	s.publish(ctx, id)
	return true, nil
}

// Feedback — оценка визита 👍/👎 (value "up" | "down" | "" — снять). userID nil — из Telegram.
func (s *Service) Feedback(ctx context.Context, id int64, value string, userID *int64) (bool, error) {
	var v *string
	if value != "" {
		v = &value
	}
	tag, err := s.DB.Exec(ctx, `UPDATE visits SET feedback=$2, feedback_by=$3, feedback_at=now() WHERE id=$1`, id, v, userID)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	s.publish(ctx, id)
	return true, nil
}
