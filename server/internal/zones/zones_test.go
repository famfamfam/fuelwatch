package zones

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name, in, wantErr string
	}{
		{"ok", `[{"type":"MONITOR","points":[[0.1,0.1],[0.9,0.1],[0.9,0.9]]},{"type":"IGNORE","points":[[0,0],[0.2,0],[0.2,0.2]]}]`, ""},
		{"no monitor", `[{"type":"IGNORE","points":[[0,0],[0.2,0],[0.2,0.2]]}]`, "MONITOR"},
		{"empty", `[]`, "MONITOR"},
		{"bad type", `[{"type":"ZONE","points":[[0,0],[1,0],[1,1]]}]`, "тип"},
		{"two points", `[{"type":"MONITOR","points":[[0,0],[1,0]]}]`, "точек"},
		{"out of range", `[{"type":"MONITOR","points":[[0,0],[1.2,0],[1,1]]}]`, "0..1"},
		{"degenerate", `[{"type":"MONITOR","points":[[0,0],[0.5,0.5],[1,1]]}]`, "площадь"},
		{"garbage", `{"a":1}`, "формат"},
		{"bowtie is allowed", `[{"type":"MONITOR","points":[[0.1,0.1],[0.5,0.5],[0.5,0.1],[0.1,0.5]]}]`, ""},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.in))
		if c.wantErr == "" && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.wantErr, err)
		}
	}
}

func TestMonitorBounds(t *testing.T) {
	zs := []Zone{
		{Type: Monitor, Points: [][2]float64{{0.2, 0.5}, {0.6, 0.5}, {0.6, 0.9}}},
		{Type: Ignore, Points: [][2]float64{{0, 0}, {1, 0}, {1, 1}}},
		{Type: Monitor, Points: [][2]float64{{0.5, 0.4}, {0.95, 0.4}, {0.95, 0.7}}},
	}
	x0, y0, x1, y1, ok := MonitorBounds(zs, 0.1)
	if !ok {
		t.Fatal("ok=false")
	}
	near := func(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }
	if !near(x0, 0.1) || !near(y0, 0.3) || !near(x1, 1) || !near(y1, 1) {
		t.Errorf("bounds = %v %v %v %v", x0, y0, x1, y1)
	}
}
