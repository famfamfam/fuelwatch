// Package zones — зоны MONITOR/IGNORE: полигоны в нормализованных [0..1] координатах повёрнутого полного кадра.
package zones

import (
	"encoding/json"
	"fmt"
)

const (
	Monitor = "MONITOR"
	Ignore  = "IGNORE"

	MaxZones  = 20
	MinPoints = 3
	MaxPoints = 64
)

type Zone struct {
	Type   string       `json:"type"`
	Points [][2]float64 `json:"points"`
}

// Parse разбирает и проверяет список зон. Ошибка — понятная оператору.
func Parse(raw json.RawMessage) ([]Zone, error) {
	var zs []Zone
	if err := json.Unmarshal(raw, &zs); err != nil {
		return nil, fmt.Errorf("формат зон: %w", err)
	}
	return zs, Validate(zs)
}

func Validate(zs []Zone) error {
	if len(zs) > MaxZones {
		return fmt.Errorf("не больше %d зон", MaxZones)
	}
	monitors := 0
	for i, z := range zs {
		switch z.Type {
		case Monitor:
			monitors++
		case Ignore:
		default:
			return fmt.Errorf("зона %d: тип %q, нужен MONITOR или IGNORE", i+1, z.Type)
		}
		if len(z.Points) < MinPoints || len(z.Points) > MaxPoints {
			return fmt.Errorf("зона %d: от %d до %d точек", i+1, MinPoints, MaxPoints)
		}
		for _, p := range z.Points {
			if p[0] < 0 || p[0] > 1 || p[1] < 0 || p[1] > 1 {
				return fmt.Errorf("зона %d: координаты должны быть в диапазоне 0..1", i+1)
			}
		}
		if spread(z.Points) < 1e-6 {
			return fmt.Errorf("зона %d: нулевая площадь", i+1)
		}
	}
	if monitors == 0 {
		return fmt.Errorf("нужна хотя бы одна зона MONITOR")
	}
	return nil
}

// spread — сумма модулей площадей треугольников веером от первой точки: 0 только если все точки
// на одной прямой. Обычная площадь не годится: у самопересекающейся «бабочки» она равна нулю.
func spread(pts [][2]float64) float64 {
	a := 0.0
	o := pts[0]
	for i := 1; i < len(pts)-1; i++ {
		c := (pts[i][0]-o[0])*(pts[i+1][1]-o[1]) - (pts[i+1][0]-o[0])*(pts[i][1]-o[1])
		if c < 0 {
			c = -c
		}
		a += c
	}
	return a / 2
}

// MonitorBounds — прямоугольник вокруг всех MONITOR с запасом margin, в [0..1]. ok=false, если MONITOR нет.
func MonitorBounds(zs []Zone, margin float64) (x0, y0, x1, y1 float64, ok bool) {
	x0, y0, x1, y1 = 1, 1, 0, 0
	for _, z := range zs {
		if z.Type != Monitor {
			continue
		}
		for _, p := range z.Points {
			x0, y0 = min(x0, p[0]), min(y0, p[1])
			x1, y1 = max(x1, p[0]), max(y1, p[1])
			ok = true
		}
	}
	if !ok {
		return 0, 0, 1, 1, false
	}
	return max(0, x0-margin), max(0, y0-margin), min(1, x1+margin), min(1, y1+margin), true
}
