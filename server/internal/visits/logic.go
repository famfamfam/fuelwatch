// Package visits — логика визита бензовоза (D-09, docs/05-backend.md §5).
// Step — чистая функция без БД: табличные тесты в logic_test.go.
package visits

import (
	"time"

	"fuelwatch/internal/settings"
)

type Params struct {
	MinConfidence  float64
	ConfirmN       int
	ConfirmWindow  time.Duration
	ExtraFrames    int
	LeaveN         int
	LeaveMin       time.Duration
	UnloadingAfter time.Duration // 0 — не уведомлять
	MaxDuration    time.Duration
}

func ParamsFrom(v settings.Values) Params {
	return Params{
		MinConfidence:  v.Float("visit.min_confidence"),
		ConfirmN:       v.Int("visit.confirm_n"),
		ConfirmWindow:  v.Seconds("visit.confirm_window_s"),
		ExtraFrames:    v.Int("visit.extra_frames"),
		LeaveN:         v.Int("visit.leave_n"),
		LeaveMin:       v.Seconds("visit.leave_min_s"),
		UnloadingAfter: time.Duration(v.Int("visit.unloading_after_min")) * time.Minute,
		MaxDuration:    time.Duration(v.Int("visit.max_hours")) * time.Hour,
	}
}

// Obs — то, что логике нужно от наблюдения.
type Obs struct {
	TankerPresent, TankerInZone, Obstructed bool
	Confidence                              float64
	FrameID                                 string
}

type Visit struct {
	StartedAt, LastSeenAt time.Time
	Negatives             int
	UnloadingNotified     bool
	FirstFrameID          string
	LastFrameID           string
}

type State struct {
	Open         *Visit
	PendingCount int
	PendingSince *time.Time
	PendingFrame string // первый положительный кадр кандидата
}

type Event string

const (
	Arrived   Event = "arrived"
	Unloading Event = "unloading"
	Left      Event = "left"
)

type Decision struct {
	State       State
	Event       Event     // "" — ничего не произошло
	EndedAt     time.Time // для Left
	ExtraFrames int       // сколько доп. кадров попросить у телефона
}

func (o Obs) positive(p Params) bool {
	return o.TankerPresent && o.TankerInZone && o.Confidence >= p.MinConfidence
}

// Step применяет одно наблюдение с временем кадра t.
func Step(st State, o Obs, t time.Time, p Params) Decision {
	d := Decision{State: st}
	// Вид закрыт — наблюдение ничего не говорит о бензовозе.
	if o.Obstructed {
		return d
	}
	if st.Open == nil {
		if !o.positive(p) {
			d.State.PendingCount, d.State.PendingSince, d.State.PendingFrame = 0, nil, ""
			return d
		}
		if st.PendingSince == nil || t.Sub(*st.PendingSince) > p.ConfirmWindow {
			since := t
			d.State.PendingCount, d.State.PendingSince, d.State.PendingFrame = 1, &since, o.FrameID
		} else {
			d.State.PendingCount++
		}
		if d.State.PendingCount >= p.ConfirmN {
			d.State.Open = &Visit{
				StartedAt: *d.State.PendingSince, LastSeenAt: t,
				FirstFrameID: d.State.PendingFrame, LastFrameID: o.FrameID,
			}
			d.State.PendingCount, d.State.PendingSince, d.State.PendingFrame = 0, nil, ""
			d.Event = Arrived
			return d
		}
		d.ExtraFrames = p.ExtraFrames
		return d
	}

	v := *st.Open
	d.State.Open = &v
	if o.positive(p) {
		if t.After(v.LastSeenAt) {
			v.LastSeenAt, v.LastFrameID = t, o.FrameID
		}
		v.Negatives = 0
		if p.UnloadingAfter > 0 && !v.UnloadingNotified && t.Sub(v.StartedAt) >= p.UnloadingAfter {
			v.UnloadingNotified = true
			d.Event = Unloading
		}
		return d
	}
	v.Negatives++
	if v.Negatives >= p.LeaveN && t.Sub(v.LastSeenAt) >= p.LeaveMin {
		d.State.Open = nil
		d.Event = Left
		d.EndedAt = v.LastSeenAt
		return d
	}
	d.ExtraFrames = p.ExtraFrames
	return d
}

// Stale — визит открыт дольше visit.max_hours: закрыть (предохранитель от «залипания»).
func Stale(v Visit, now time.Time, p Params) bool {
	return p.MaxDuration > 0 && now.Sub(v.StartedAt) > p.MaxDuration
}
