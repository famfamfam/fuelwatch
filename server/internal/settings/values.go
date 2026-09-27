package settings

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"time"
)

// Values — итоговые значения по ключам. Числа из JSON и БД приходят как float64.
type Values map[string]any

// Caps — часть camera_caps телефона, нужная для проверки настроек.
type Caps struct {
	ZoomMin     float64  `json:"zoom_min"`
	ZoomMax     float64  `json:"zoom_max"`
	Resolutions []string `json:"resolutions"`
	ManualFocus bool     `json:"manual_focus"`
}

func ParseCaps(raw []byte) *Caps {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var c Caps
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil
	}
	return &c
}

// Resolve сливает слои: default → global → device (только для Overridable).
// Возвращает значения и источник каждого: "default" | "global" | "device".
func Resolve(global, device map[string]any) (Values, map[string]string) {
	vals := make(Values, len(registry))
	src := make(map[string]string, len(registry))
	for _, d := range registry {
		v, s := d.Default, "default"
		if g, ok := global[d.Key]; ok {
			v, s = g, "global"
		}
		if d.Overridable {
			if dv, ok := device[d.Key]; ok {
				v, s = dv, "device"
			}
		}
		vals[d.Key] = v
		src[d.Key] = s
	}
	return vals, src
}

// DeviceScoped — только настройки, которые уходят на телефон.
func (v Values) DeviceScoped() map[string]any {
	out := map[string]any{}
	for _, d := range registry {
		if d.Scope == ScopeDevice {
			out[d.Key] = v[d.Key]
		}
	}
	return out
}

func toFloat(x any) (float64, bool) {
	switch n := x.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

func (v Values) Float(key string) float64 {
	if f, ok := toFloat(v[key]); ok {
		return f
	}
	d, _ := toFloat(byKey[key].Default)
	return d
}

func (v Values) Int(key string) int { return int(math.Round(v.Float(key))) }

// Seconds — для ключей *_s.
func (v Values) Seconds(key string) time.Duration {
	return time.Duration(v.Float(key) * float64(time.Second))
}

// Days — для ключей *_days.
func (v Values) Days(key string) time.Duration { return time.Duration(v.Int(key)) * 24 * time.Hour }

func (v Values) Bool(key string) bool {
	if b, ok := v[key].(bool); ok {
		return b
	}
	b, _ := byKey[key].Default.(bool)
	return b
}

func (v Values) String(key string) string {
	if s, ok := v[key].(string); ok {
		return s
	}
	s, _ := byKey[key].Default.(string)
	return s
}

// Strings — для string_list и enum_list.
func (v Values) Strings(key string) []string {
	switch l := v[key].(type) {
	case []string:
		return l
	case []any:
		out := make([]string, 0, len(l))
		for _, it := range l {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	d, _ := byKey[key].Default.([]string)
	return d
}

var resolutionRe = regexp.MustCompile(`^\d{2,5}x\d{2,5}$`)

// Normalize проверяет значение по определению и возвращает его в каноническом виде.
// caps — возможности камеры устройства (nil для глобальных значений).
func (d Def) Normalize(x any, caps *Caps) (any, error) {
	switch d.Type {
	case Int, Float:
		n, ok := toFloat(x)
		if !ok {
			return nil, fmt.Errorf("нужно число")
		}
		if d.Type == Int && n != math.Trunc(n) {
			return nil, fmt.Errorf("нужно целое число")
		}
		lo, hi := d.Min, d.Max
		if d.Caps == CapsZoom && caps != nil && caps.ZoomMax > 0 {
			lo, hi = &caps.ZoomMin, &caps.ZoomMax
		}
		if lo != nil && n < *lo {
			return nil, fmt.Errorf("не меньше %g", *lo)
		}
		if hi != nil && n > *hi {
			return nil, fmt.Errorf("не больше %g", *hi)
		}
		if d.Type == Int {
			return int(n), nil
		}
		return n, nil
	case Bool:
		b, ok := x.(bool)
		if !ok {
			return nil, fmt.Errorf("нужно true или false")
		}
		return b, nil
	case String:
		s, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("нужна строка")
		}
		return s, nil
	case Enum:
		s, ok := x.(string)
		if !ok {
			return nil, fmt.Errorf("нужна строка")
		}
		switch d.Caps {
		case CapsResolutions:
			if !resolutionRe.MatchString(s) {
				return nil, fmt.Errorf("формат ШИРИНАxВЫСОТА")
			}
			if caps != nil && len(caps.Resolutions) > 0 && !slices.Contains(caps.Resolutions, s) {
				return nil, fmt.Errorf("камера не поддерживает %s", s)
			}
			return s, nil
		case CapsManualFocus:
			if s == "infinity" && caps != nil && !caps.ManualFocus {
				return nil, fmt.Errorf("камера не поддерживает ручной фокус")
			}
		}
		if !slices.Contains(d.Options, s) {
			return nil, fmt.Errorf("допустимо: %v", d.Options)
		}
		return s, nil
	case StringList, EnumList:
		var arr []any
		switch v := x.(type) {
		case []any:
			arr = v
		case []string:
			for _, it := range v {
				arr = append(arr, it)
			}
		default:
			return nil, fmt.Errorf("нужен список")
		}
		out := make([]string, 0, len(arr))
		for _, it := range arr {
			s, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("элементы списка — строки")
			}
			if d.Type == EnumList && !slices.Contains(d.Options, s) {
				return nil, fmt.Errorf("недопустимое значение %q", s)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("неизвестный тип %s", d.Type)
}

// CheckRules — связанные правила между настройками. Ключ ошибки — настройка, которую надо поправить.
func CheckRules(v Values) map[string]string {
	errs := map[string]string{}
	if v.Seconds("health.offline_after_s") < 3*v.Seconds("net.heartbeat_interval_s") {
		errs["health.offline_after_s"] = "должно быть не меньше 3 × net.heartbeat_interval_s"
	}
	return errs
}

// ForCaps — копия определения с диапазоном и вариантами из возможностей камеры (для схемы в панели).
func (d Def) ForCaps(caps *Caps) Def {
	if caps == nil {
		return d
	}
	switch d.Caps {
	case CapsZoom:
		if caps.ZoomMax > 0 {
			d.Min, d.Max = f(caps.ZoomMin), f(caps.ZoomMax)
		}
	case CapsResolutions:
		d.Options = caps.Resolutions
	case CapsManualFocus:
		if !caps.ManualFocus {
			d.Options = []string{"auto"}
		}
	}
	return d
}
