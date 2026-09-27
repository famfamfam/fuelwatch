package health

import (
	"reflect"
	"testing"
	"time"
)

func fp(v float64) *float64 { return &v }
func bp(v bool) *bool       { return &v }
func ip(v int) *int         { return &v }

var lim = Limits{LowBattery: 25, OverheatC: 45, CameraStaleS: 120}

func TestFromHeartbeat(t *testing.T) {
	cases := []struct {
		name   string
		hb     HB
		paused bool
		want   map[string]Action
	}{
		{"all good", HB{Battery: fp(80), Charging: bp(true), BatteryTempC: fp(35), ThermalStatus: ip(0), LastFrameAgeS: fp(3)},
			false, map[string]Action{"POWER_OFF": Close, "LOW_BATTERY": Close, "OVERHEAT": Close, "CAMERA_STALE": Close}},
		{"unplugged, battery still fine", HB{Battery: fp(80), Charging: bp(false)},
			false, map[string]Action{"POWER_OFF": Open, "LOW_BATTERY": Close}},
		{"low battery on battery", HB{Battery: fp(20), Charging: bp(false)},
			false, map[string]Action{"POWER_OFF": Open, "LOW_BATTERY": Open}},
		{"battery between low and hysteresis keeps state", HB{Battery: fp(30), Charging: bp(false)},
			false, map[string]Action{"POWER_OFF": Open}},
		{"hot battery", HB{BatteryTempC: fp(46)}, false, map[string]Action{"OVERHEAT": Open}},
		{"severe thermal status", HB{BatteryTempC: fp(38), ThermalStatus: ip(3)}, false, map[string]Action{"OVERHEAT": Open}},
		{"cooling but within hysteresis keeps", HB{BatteryTempC: fp(43)}, false, map[string]Action{}},
		{"camera stale", HB{LastFrameAgeS: fp(300)}, false, map[string]Action{"CAMERA_STALE": Open}},
		{"paused: only power opens", HB{Battery: fp(10), Charging: bp(false), BatteryTempC: fp(50), LastFrameAgeS: fp(999)},
			true, map[string]Action{"POWER_OFF": Open}},
		{"paused: closing still works", HB{Battery: fp(90), Charging: bp(true), BatteryTempC: fp(30), LastFrameAgeS: fp(1)},
			true, map[string]Action{"POWER_OFF": Close, "LOW_BATTERY": Close, "OVERHEAT": Close, "CAMERA_STALE": Close}},
		{"nothing reported", HB{}, false, map[string]Action{}},
	}
	for _, c := range cases {
		got := FromHeartbeat(c.hb, lim, c.paused)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFromObservation(t *testing.T) {
	type step struct {
		matches, obstructed bool
		want                map[string]Action
	}
	steps := []step{
		{false, false, map[string]Action{}},                     // 1 плохой
		{false, false, map[string]Action{}},                     // 2
		{false, false, map[string]Action{"VIEW_CHANGED": Open}}, // 3 = view_bad_n
		{true, true, map[string]Action{}},                       // закрыт: вид не оцениваем, blocked=1
		{true, false, map[string]Action{}},                      // 1 хороший
		{true, false, map[string]Action{"VIEW_CHANGED": Close, "VIEW_BLOCKED": Close}},
		{true, true, map[string]Action{}},
		{true, true, map[string]Action{}},
		{true, true, map[string]Action{"VIEW_BLOCKED": Open}},
	}
	var s Streak
	for i, st := range steps {
		var got map[string]Action
		s, got = FromObservation(s, st.matches, st.obstructed, 3, false)
		for k, v := range st.want {
			if got[k] != v {
				t.Errorf("step %d: %s = %v, want %v (all %v)", i, k, got[k], v, got)
			}
		}
		if got["VIEW_CHANGED"] == Open && st.want["VIEW_CHANGED"] != Open {
			t.Errorf("step %d: unexpected VIEW_CHANGED open", i)
		}
	}
	_, got := FromObservation(Streak{Changed: 5}, false, false, 3, true)
	if got["VIEW_CHANGED"] == Open {
		t.Error("paused must not open VIEW_CHANGED")
	}
}

func TestTheft(t *testing.T) {
	t0 := time.Date(2026, 9, 27, 3, 0, 0, 0, time.UTC)
	later := t0.Add(4 * time.Minute)
	muchLater := t0.Add(10 * time.Minute)
	if !Theft(&t0, &later, 5*time.Minute) || !Theft(&later, &t0, 5*time.Minute) {
		t.Error("within window, either order")
	}
	if Theft(&t0, &muchLater, 5*time.Minute) || Theft(nil, &later, time.Minute) {
		t.Error("outside window or no MOVED")
	}
}
