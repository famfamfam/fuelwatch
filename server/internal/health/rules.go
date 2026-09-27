package health

import (
	"time"

	"fuelwatch/internal/settings"
)

// Чистые правила здоровья (docs/05-backend.md §6) — без БД, табличные тесты в rules_test.go.

// Android PowerManager.THERMAL_STATUS_SEVERE.
const thermalSevere = 3

// Гистерезис: проблема закрывается с запасом, чтобы не мигать на границе.
const (
	batteryHysteresis = 10  // %
	tempHysteresis    = 3.0 // °C
	viewRecoverN      = 2   // столько хороших ответов подряд закрывают VIEW_*
)

type Action int

const (
	Keep Action = iota
	Open
	Close
)

// HB — поля heartbeat, нужные правилам. nil — телефон не прислал значение.
type HB struct {
	Battery       *float64 `json:"battery"`
	Charging      *bool    `json:"charging"`
	BatteryTempC  *float64 `json:"battery_temp_c"`
	ThermalStatus *int     `json:"thermal_status"`
	LastFrameAgeS *float64 `json:"last_frame_age_s"`
}

type Limits struct {
	LowBattery   float64
	OverheatC    float64
	CameraStaleS float64
}

func LimitsFrom(v settings.Values) Limits {
	return Limits{
		LowBattery:   v.Float("health.low_battery_pct"),
		OverheatC:    v.Float("health.overheat_c"),
		CameraStaleS: v.Float("health.camera_stale_s"),
	}
}

// FromHeartbeat — что делать с проблемами, которые видны по heartbeat.
// paused: в режиме обслуживания открываются только OFFLINE и POWER_OFF (закрываться могут все).
func FromHeartbeat(hb HB, l Limits, paused bool) map[string]Action {
	out := map[string]Action{}
	set := func(typ string, a Action) {
		if a == Open && paused && typ != "POWER_OFF" {
			return
		}
		out[typ] = a
	}
	if hb.Charging != nil {
		if *hb.Charging {
			set("POWER_OFF", Close)
		} else {
			set("POWER_OFF", Open)
		}
	}
	if hb.Battery != nil {
		charging := hb.Charging != nil && *hb.Charging
		switch {
		case *hb.Battery <= l.LowBattery && !charging:
			set("LOW_BATTERY", Open)
		case *hb.Battery > l.LowBattery+batteryHysteresis || charging:
			set("LOW_BATTERY", Close)
		}
	}
	hot := hb.BatteryTempC != nil && *hb.BatteryTempC >= l.OverheatC
	severe := hb.ThermalStatus != nil && *hb.ThermalStatus >= thermalSevere
	// Без данных о температуре перегрев не закрываем.
	cool := (hb.BatteryTempC != nil || hb.ThermalStatus != nil) &&
		(hb.BatteryTempC == nil || *hb.BatteryTempC < l.OverheatC-tempHysteresis) &&
		(hb.ThermalStatus == nil || *hb.ThermalStatus < thermalSevere)
	switch {
	case hot || severe:
		set("OVERHEAT", Open)
	case cool:
		set("OVERHEAT", Close)
	}
	if hb.LastFrameAgeS != nil {
		if *hb.LastFrameAgeS > l.CameraStaleS {
			set("CAMERA_STALE", Open)
		} else {
			set("CAMERA_STALE", Close)
		}
	}
	return out
}

// Streak — счётчики подряд идущих ответов VLM о виде камеры.
type Streak struct {
	Changed, ChangedOK, Blocked, BlockedOK int
}

// FromObservation — вид камеры по ответу модели: badN плохих подряд открывают, viewRecoverN хороших подряд закрывают.
func FromObservation(s Streak, viewMatches, obstructed bool, badN int, paused bool) (Streak, map[string]Action) {
	out := map[string]Action{}
	if obstructed {
		s.Blocked++
		s.BlockedOK = 0
	} else {
		s.BlockedOK++
		s.Blocked = 0
	}
	// Закрытый вид ничего не говорит о положении камеры.
	if !obstructed {
		if viewMatches {
			s.ChangedOK++
			s.Changed = 0
		} else {
			s.Changed++
			s.ChangedOK = 0
		}
	}
	if s.Blocked >= badN && !paused {
		out["VIEW_BLOCKED"] = Open
	} else if s.BlockedOK >= viewRecoverN {
		out["VIEW_BLOCKED"] = Close
	}
	if obstructed {
		return s, out
	}
	if s.Changed >= badN && !paused {
		out["VIEW_CHANGED"] = Open
	} else if s.ChangedOK >= viewRecoverN {
		out["VIEW_CHANGED"] = Close
	}
	return s, out
}

// Theft — телефон сдвинули, и в пределах окна пропало питание или связь (в любом порядке).
func Theft(movedAt, lostAt *time.Time, window time.Duration) bool {
	if movedAt == nil || lostAt == nil {
		return false
	}
	d := lostAt.Sub(*movedAt)
	if d < 0 {
		d = -d
	}
	return d <= window
}
