package settings

import (
	"testing"
)

func TestRegistryDefaultsAreValid(t *testing.T) {
	for _, d := range All() {
		v := d.Default
		// Числа по умолчанию в коде — int/float64; проверяем, что они проходят собственную валидацию.
		if _, err := d.Normalize(v, nil); err != nil {
			t.Errorf("%s: default %v invalid: %v", d.Key, v, err)
		}
		if d.Group == "" || d.Label == "" || (d.Scope != ScopeDevice && d.Scope != ScopeServer) {
			t.Errorf("%s: group, label and scope are required", d.Key)
		}
	}
	vals, _ := Resolve(nil, nil)
	if errs := CheckRules(vals); len(errs) > 0 {
		t.Errorf("defaults violate rules: %v", errs)
	}
}

func TestNormalize(t *testing.T) {
	d, _ := Lookup("capture.keyframe_interval_s")
	zoom, _ := Lookup("capture.zoom")
	res, _ := Lookup("capture.resolution")
	focus, _ := Lookup("capture.focus_mode")
	caps := &Caps{ZoomMin: 1, ZoomMax: 5, Resolutions: []string{"1920x1080", "1280x720"}, ManualFocus: false}

	cases := []struct {
		name    string
		def     Def
		in      any
		caps    *Caps
		want    any
		wantErr bool
	}{
		{"int from json float", d, 60.0, nil, 60, false},
		{"int fraction", d, 60.5, nil, nil, true},
		{"int below min", d, 10.0, nil, nil, true},
		{"int above max", d, 4000.0, nil, nil, true},
		{"int wrong type", d, "60", nil, nil, true},
		{"zoom within caps", zoom, 4.5, caps, 4.5, false},
		{"zoom above caps", zoom, 6.0, caps, nil, true},
		{"zoom global range", zoom, 6.0, nil, 6.0, false},
		{"resolution supported", res, "1280x720", caps, "1280x720", false},
		{"resolution unsupported", res, "640x480", caps, nil, true},
		{"resolution bad format", res, "hd", nil, nil, true},
		{"focus infinity without manual", focus, "infinity", caps, nil, true},
		{"focus infinity global", focus, "infinity", nil, "infinity", false},
		{"focus unknown", focus, "macro", nil, nil, true},
	}
	for _, c := range cases {
		got, err := c.def.Normalize(c.in, c.caps)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", c.name, err, c.wantErr)
			continue
		}
		if !c.wantErr && got != c.want {
			t.Errorf("%s: got %v (%T), want %v (%T)", c.name, got, got, c.want, c.want)
		}
	}
}

func TestResolveLayers(t *testing.T) {
	global := map[string]any{"capture.zoom": 2.0, "storage.frame_days": 30.0}
	device := map[string]any{"capture.zoom": 3.0, "storage.frame_days": 1.0} // storage — только глобально
	vals, src := Resolve(global, device)
	if vals.Float("capture.zoom") != 3 || src["capture.zoom"] != "device" {
		t.Errorf("zoom: %v from %s", vals["capture.zoom"], src["capture.zoom"])
	}
	if vals.Int("storage.frame_days") != 30 || src["storage.frame_days"] != "global" {
		t.Errorf("frame_days: %v from %s", vals["storage.frame_days"], src["storage.frame_days"])
	}
	if vals.Int("capture.sample_interval_s") != 15 || src["capture.sample_interval_s"] != "default" {
		t.Errorf("sample_interval: %v", vals["capture.sample_interval_s"])
	}
	if _, ok := vals.DeviceScoped()["health.offline_after_s"]; ok {
		t.Error("server-scoped key leaked to device config")
	}
}

func TestRules(t *testing.T) {
	vals, _ := Resolve(map[string]any{"net.heartbeat_interval_s": 60.0, "health.offline_after_s": 120.0}, nil)
	if _, ok := CheckRules(vals)["health.offline_after_s"]; !ok {
		t.Error("offline_after_s < 3 × heartbeat must be rejected")
	}
}

func TestValidateDeviceLevel(t *testing.T) {
	_, errs := validate(map[string]any{"storage.frame_days": 3.0, "nope": 1.0, "capture.zoom": nil}, nil, true)
	if errs["storage.frame_days"] == "" || errs["nope"] == "" || errs["capture.zoom"] != "" {
		t.Errorf("unexpected errors: %v", errs)
	}
}
