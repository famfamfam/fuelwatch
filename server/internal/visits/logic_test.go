package visits

import (
	"testing"
	"time"
)

var p = Params{
	MinConfidence: 0.7, ConfirmN: 2, ConfirmWindow: 5 * time.Minute, ExtraFrames: 3,
	LeaveN: 2, LeaveMin: time.Minute, UnloadingAfter: 10 * time.Minute, MaxDuration: 4 * time.Hour,
}

var t0 = time.Date(2026, 9, 27, 14, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

var (
	yes     = Obs{TankerPresent: true, TankerInZone: true, Confidence: 0.9}
	no      = Obs{Confidence: 0.9}
	unsure  = Obs{TankerPresent: true, TankerInZone: true, Confidence: 0.5}
	outside = Obs{TankerPresent: true, TankerInZone: false, Confidence: 0.95}
	blocked = Obs{TankerPresent: false, Obstructed: true}
)

type step struct {
	obs   Obs
	sec   int
	event Event
	extra int
	open  bool
}

func run(t *testing.T, name string, steps []step) State {
	t.Helper()
	var st State
	for i, s := range steps {
		d := Step(st, s.obs, at(s.sec), p)
		if d.Event != s.event || d.ExtraFrames != s.extra || (d.State.Open != nil) != s.open {
			t.Errorf("%s step %d: event=%q extra=%d open=%v, want event=%q extra=%d open=%v",
				name, i, d.Event, d.ExtraFrames, d.State.Open != nil, s.event, s.extra, s.open)
		}
		st = d.State
	}
	return st
}

func TestVisitLifecycle(t *testing.T) {
	st := run(t, "full visit", []step{
		{no, 0, "", 0, false},
		{yes, 15, "", 3, false},        // первый положительный — просим ещё кадры
		{yes, 25, Arrived, 0, true},    // второй — приехал
		{yes, 400, "", 0, true},        // стоит
		{yes, 640, Unloading, 0, true}, // ≥10 мин от начала (15 с) — разгрузка
		{yes, 900, "", 0, true},        // повторно не уведомляем
		{no, 1500, "", 3, true},        // первый отрицательный — просим кадры
		{no, 1510, Left, 0, false},     // второй подряд и ≥60 с с последнего положительного — уехал
	})
	if st.Open != nil || st.PendingCount != 0 {
		t.Errorf("state after leave: %+v", st)
	}
}

func TestArrivalStartsAtFirstPositive(t *testing.T) {
	var st State
	st = Step(st, yes, at(100), p).State
	d := Step(st, yes, at(130), p)
	if d.Event != Arrived || !d.State.Open.StartedAt.Equal(at(100)) || !d.State.Open.LastSeenAt.Equal(at(130)) {
		t.Errorf("visit %+v", d.State.Open)
	}
}

func TestNoiseDoesNotOpenVisit(t *testing.T) {
	run(t, "single positive then negative", []step{
		{yes, 0, "", 3, false},
		{no, 10, "", 0, false},
		{yes, 20, "", 3, false}, // счёт начинается заново
	})
	run(t, "positives too far apart", []step{
		{yes, 0, "", 3, false},
		{yes, 400, "", 3, false}, // вне окна 5 мин — снова первый
		{yes, 420, Arrived, 0, true},
	})
	run(t, "low confidence and outside zone are negative", []step{
		{unsure, 0, "", 0, false},
		{outside, 10, "", 0, false},
		{unsure, 20, "", 0, false},
	})
}

func TestLeaveNeedsTimeSinceLastPositive(t *testing.T) {
	run(t, "two negatives right after positive", []step{
		{yes, 0, "", 3, false},
		{yes, 10, Arrived, 0, true},
		{no, 20, "", 3, true},
		{no, 30, "", 3, true},    // 2 подряд, но с последнего положительного 20 с < 60 с
		{no, 80, Left, 0, false}, // уже 70 с
	})
	run(t, "positive in between resets negatives", []step{
		{yes, 0, "", 3, false},
		{yes, 10, Arrived, 0, true},
		{no, 100, "", 3, true},
		{yes, 110, "", 0, true},
		{no, 200, "", 3, true},
		{no, 210, Left, 0, false},
	})
}

func TestLeftEndsAtLastPositive(t *testing.T) {
	var st State
	for _, s := range []struct {
		o   Obs
		sec int
	}{{yes, 0}, {yes, 10}, {yes, 300}, {no, 600}} {
		st = Step(st, s.o, at(s.sec), p).State
	}
	d := Step(st, no, at(620), p)
	if d.Event != Left || !d.EndedAt.Equal(at(300)) {
		t.Errorf("event %q ended %v, want left at %v", d.Event, d.EndedAt, at(300))
	}
}

func TestObstructedIsIgnored(t *testing.T) {
	run(t, "blocked view neither confirms nor ends", []step{
		{yes, 0, "", 3, false},
		{blocked, 10, "", 0, false},
		{yes, 20, Arrived, 0, true},
		{blocked, 30, "", 0, true},
		{blocked, 300, "", 0, true},
		{no, 400, "", 3, true},
		{no, 410, Left, 0, false},
	})
}

func TestUnloadingCanBeDisabled(t *testing.T) {
	q := p
	q.UnloadingAfter = 0
	var st State
	st = Step(st, yes, at(0), q).State
	st = Step(st, yes, at(10), q).State
	if d := Step(st, yes, at(3600), q); d.Event != "" {
		t.Errorf("unloading disabled, got %q", d.Event)
	}
}

func TestConfirmOne(t *testing.T) {
	q := p
	q.ConfirmN = 1
	if d := Step(State{}, yes, at(0), q); d.Event != Arrived {
		t.Errorf("confirm_n=1: %q", d.Event)
	}
}

func TestStale(t *testing.T) {
	v := Visit{StartedAt: at(0)}
	if Stale(v, at(3*3600), p) || !Stale(v, at(5*3600), p) {
		t.Error("stale threshold")
	}
}
