// Package settings — реестр настроек (docs/08-config.md), слои default → global → device,
// проверка значений и config_version.
package settings

type Type string

const (
	Int        Type = "int"
	Float      Type = "float"
	Bool       Type = "bool"
	String     Type = "string"
	Enum       Type = "enum"
	StringList Type = "string_list"
	EnumList   Type = "enum_list"
)

type Scope string

const (
	ScopeDevice Scope = "device" // уходит на телефон в конфиге
	ScopeServer Scope = "server" // только сервер
)

// Caps — откуда брать допустимые значения из camera_caps устройства.
type CapsSource string

const (
	CapsNone        CapsSource = ""
	CapsZoom        CapsSource = "zoom"
	CapsResolutions CapsSource = "resolutions"
	CapsManualFocus CapsSource = "manual_focus"
)

type Def struct {
	Key           string     `json:"key"`
	Type          Type       `json:"type"`
	Default       any        `json:"default"`
	Min           *float64   `json:"min,omitempty"`
	Max           *float64   `json:"max,omitempty"`
	Step          *float64   `json:"step,omitempty"`
	Unit          string     `json:"unit,omitempty"`
	Options       []string   `json:"options,omitempty"`
	Group         string     `json:"group"`
	Label         string     `json:"label"`
	Help          string     `json:"help,omitempty"`
	Scope         Scope      `json:"scope"`
	Overridable   bool       `json:"overridable"`
	CameraRestart bool       `json:"camera_restart,omitempty"`
	NewReference  bool       `json:"new_reference,omitempty"`
	Caps          CapsSource `json:"caps,omitempty"`
}

func f(v float64) *float64 { return &v }

// Группы в порядке вкладок панели.
var Groups = []struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}{
	{"camera", "Камера"},
	{"detector", "Сравнение кадров"},
	{"network", "Связь"},
	{"live", "Живой режим"},
	{"visit", "Визиты"},
	{"health", "Здоровье"},
	{"vision", "VLM"},
	{"notify", "Уведомления"},
	{"storage", "Хранение"},
}

// registry повторяет docs/08-config.md. Новая настройка = строка здесь + строка в документе.
var registry = []Def{
	// Камера
	{Key: "capture.sample_interval_s", Type: Int, Default: 15, Min: f(5), Max: f(120), Step: f(1), Unit: "с",
		Group: "camera", Label: "Интервал анализа", Help: "Как часто телефон анализирует кадр",
		Scope: ScopeDevice, Overridable: true},
	{Key: "capture.keyframe_interval_s", Type: Int, Default: 300, Min: f(30), Max: f(3600), Step: f(30), Unit: "с",
		Group: "camera", Label: "Интервал плановых кадров", Help: "Как часто телефон присылает кадр, даже если ничего не изменилось",
		Scope: ScopeDevice, Overridable: true},
	{Key: "capture.max_uploads_per_hour", Type: Int, Default: 40, Min: f(5), Max: f(500), Step: f(5),
		Group: "camera", Label: "Лимит загрузок в час", Help: "Только кадры по изменению. Плановые, запрошенные сервером и живой режим — вне лимита",
		Scope: ScopeDevice, Overridable: true},
	{Key: "capture.resolution", Type: Enum, Default: "1920x1080",
		Group: "camera", Label: "Разрешение", Help: "Разрешение потока камеры. Варианты — из возможностей телефона",
		Scope: ScopeDevice, Overridable: true, CameraRestart: true, NewReference: true, Caps: CapsResolutions},
	{Key: "capture.zoom", Type: Float, Default: 1.0, Min: f(1), Max: f(10), Step: f(0.1), Unit: "×",
		Group: "camera", Label: "Zoom", Help: "Диапазон — из возможностей телефона",
		Scope: ScopeDevice, Overridable: true, NewReference: true, Caps: CapsZoom},
	{Key: "capture.focus_mode", Type: Enum, Default: "auto", Options: []string{"auto", "infinity"},
		Group: "camera", Label: "Фокус", Help: "«infinity» — только если телефон поддерживает ручной фокус",
		Scope: ScopeDevice, Overridable: true, CameraRestart: true, Caps: CapsManualFocus},
	{Key: "capture.jpeg_quality", Type: Int, Default: 75, Min: f(40), Max: f(95), Step: f(1),
		Group: "camera", Label: "Качество JPEG",
		Scope: ScopeDevice, Overridable: true},
	{Key: "capture.crop_margin", Type: Float, Default: 0.10, Min: f(0), Max: f(0.5), Step: f(0.01),
		Group: "camera", Label: "Запас вокруг зоны", Help: "Доля кадра вокруг MONITOR при вырезе",
		Scope: ScopeDevice, Overridable: true},
	{Key: "capture.keep_screen_on", Type: Bool, Default: true,
		Group: "camera", Label: "Экран всегда включён", Help: "Чёрный экран, минимальная яркость",
		Scope: ScopeDevice, Overridable: true},

	// Сравнение кадров
	{Key: "detector.change_frac", Type: Float, Default: 0.08, Min: f(0.01), Max: f(0.8), Step: f(0.01),
		Group: "detector", Label: "Доля изменившихся пикселей", Help: "Сколько зоны должно измениться, чтобы отправить кадр",
		Scope: ScopeDevice, Overridable: true},
	{Key: "detector.pixel_delta", Type: Int, Default: 18, Min: f(5), Max: f(80), Step: f(1),
		Group: "detector", Label: "Порог яркости пикселя (день)", Help: "Изменение яркости 0–255, после которого пиксель считается изменившимся",
		Scope: ScopeDevice, Overridable: true},
	{Key: "detector.pixel_delta_night", Type: Int, Default: 28, Min: f(5), Max: f(100), Step: f(1),
		Group: "detector", Label: "Порог яркости пикселя (ночь)",
		Scope: ScopeDevice, Overridable: true},
	{Key: "detector.night_luma", Type: Int, Default: 40, Min: f(5), Max: f(120), Step: f(1),
		Group: "detector", Label: "Яркость «ночи»", Help: "Средняя яркость зоны ниже — считаем, что ночь",
		Scope: ScopeDevice, Overridable: true},
	{Key: "detector.max_light_jump", Type: Float, Default: 0.35, Min: f(0.05), Max: f(1), Step: f(0.05),
		Group: "detector", Label: "Резкий скачок освещения", Help: "Если общая яркость изменилась сильнее (фары, вспышка), выборка пропускается",
		Scope: ScopeDevice, Overridable: true},

	// Связь
	{Key: "net.heartbeat_interval_s", Type: Int, Default: 20, Min: f(5), Max: f(120), Step: f(1), Unit: "с",
		Group: "network", Label: "Интервал heartbeat", Help: "Равен задержке доставки команд",
		Scope: ScopeDevice, Overridable: true},
	{Key: "net.command_ttl_s", Type: Int, Default: 600, Min: f(60), Max: f(86400), Step: f(60), Unit: "с",
		Group: "network", Label: "Срок жизни команды", Help: "Команда, не доставленная за это время, не выполняется",
		Scope: ScopeServer, Overridable: false},
	{Key: "queue.max_mb", Type: Int, Default: 300, Min: f(50), Max: f(2000), Step: f(10), Unit: "MB",
		Group: "network", Label: "Лимит очереди без сети",
		Scope: ScopeDevice, Overridable: true},

	// Живой режим
	{Key: "live.interval_s", Type: Int, Default: 5, Min: f(2), Max: f(60), Step: f(1), Unit: "с",
		Group: "live", Label: "Снимок раз в",
		Scope: ScopeDevice, Overridable: true},
	{Key: "live.heartbeat_interval_s", Type: Int, Default: 5, Min: f(2), Max: f(30), Step: f(1), Unit: "с",
		Group: "live", Label: "Heartbeat в живом режиме",
		Scope: ScopeDevice, Overridable: true},
	{Key: "live.duration_s", Type: Int, Default: 300, Min: f(30), Max: f(1800), Step: f(30), Unit: "с",
		Group: "live", Label: "Длительность", Help: "На сколько включается живой режим по кнопке",
		Scope: ScopeServer, Overridable: true},

	// Визиты (используются с шага 3)
	{Key: "visit.min_confidence", Type: Float, Default: 0.7, Min: f(0.3), Max: f(0.99), Step: f(0.01),
		Group: "visit", Label: "Порог уверенности", Help: "Ответ VLM с меньшей уверенностью не считается положительным",
		Scope: ScopeServer, Overridable: true},
	{Key: "visit.confirm_n", Type: Int, Default: 2, Min: f(1), Max: f(10), Step: f(1),
		Group: "visit", Label: "Положительных для «приехал»",
		Scope: ScopeServer, Overridable: true},
	{Key: "visit.confirm_window_s", Type: Int, Default: 300, Min: f(30), Max: f(3600), Step: f(30), Unit: "с",
		Group: "visit", Label: "Окно подтверждения",
		Scope: ScopeServer, Overridable: true},
	{Key: "visit.extra_frames", Type: Int, Default: 3, Min: f(0), Max: f(10), Step: f(1),
		Group: "visit", Label: "Доп. кадров при неясности",
		Scope: ScopeServer, Overridable: true},
	{Key: "visit.leave_n", Type: Int, Default: 2, Min: f(1), Max: f(10), Step: f(1),
		Group: "visit", Label: "Отрицательных подряд для «уехал»",
		Scope: ScopeServer, Overridable: true},
	{Key: "visit.leave_min_s", Type: Int, Default: 60, Min: f(0), Max: f(1800), Step: f(10), Unit: "с",
		Group: "visit", Label: "Мин. время с последнего положительного",
		Scope: ScopeServer, Overridable: true},
	{Key: "visit.unloading_after_min", Type: Int, Default: 10, Min: f(0), Max: f(240), Step: f(1), Unit: "мин",
		Group: "visit", Label: "«Вероятно, разгрузка» через", Help: "0 — не уведомлять",
		Scope: ScopeServer, Overridable: true},
	{Key: "visit.max_hours", Type: Int, Default: 4, Min: f(1), Max: f(24), Step: f(1), Unit: "ч",
		Group: "visit", Label: "Закрыть «зависший» визит через",
		Scope: ScopeServer, Overridable: true},
	{Key: "visit.late_after_s", Type: Int, Default: 600, Min: f(60), Max: f(86400), Step: f(60), Unit: "с",
		Group: "visit", Label: "Поздний кадр", Help: "Кадр старше этого при обработке — без уведомлений о приезде и отъезде",
		Scope: ScopeServer, Overridable: true},

	// Здоровье
	{Key: "health.offline_after_s", Type: Int, Default: 180, Min: f(60), Max: f(3600), Step: f(10), Unit: "с",
		Group: "health", Label: "Offline через", Help: "Без heartbeat дольше — проблема «нет связи». Не меньше 3 интервалов heartbeat",
		Scope: ScopeServer, Overridable: true},
	{Key: "health.camera_stale_s", Type: Int, Default: 120, Min: f(30), Max: f(1800), Step: f(10), Unit: "с",
		Group: "health", Label: "Камера не отвечает через", Help: "Телефон перезапускает камеру уже через половину этого времени",
		Scope: ScopeDevice, Overridable: true},
	{Key: "health.tilt_alert_deg", Type: Float, Default: 2.0, Min: f(0.5), Max: f(20), Step: f(0.5), Unit: "°",
		Group: "health", Label: "Порог наклона", Help: "Наклон относительно базового — «телефон сдвинули»",
		Scope: ScopeDevice, Overridable: true},
	{Key: "health.shake_ms2", Type: Float, Default: 3.0, Min: f(0.5), Max: f(20), Step: f(0.5), Unit: "м/с²",
		Group: "health", Label: "Порог тряски",
		Scope: ScopeDevice, Overridable: true},
	{Key: "health.low_battery_pct", Type: Int, Default: 25, Min: f(5), Max: f(80), Step: f(1), Unit: "%",
		Group: "health", Label: "Низкий заряд",
		Scope: ScopeServer, Overridable: true},
	{Key: "health.overheat_c", Type: Int, Default: 45, Min: f(35), Max: f(60), Step: f(1), Unit: "°C",
		Group: "health", Label: "Перегрев батареи",
		Scope: ScopeServer, Overridable: true},
	{Key: "health.view_bad_n", Type: Int, Default: 3, Min: f(1), Max: f(20), Step: f(1),
		Group: "health", Label: "Ответов VLM «вид не тот» подряд",
		Scope: ScopeServer, Overridable: true},
	{Key: "health.theft_window_s", Type: Int, Default: 300, Min: f(30), Max: f(3600), Step: f(30), Unit: "с",
		Group: "health", Label: "Окно подозрения на кражу", Help: "Сдвинули, и за это время пропало питание или связь",
		Scope: ScopeServer, Overridable: true},

	// VLM (используются с шага 3)
	{Key: "vision.provider", Type: Enum, Default: "openrouter", Options: []string{"openrouter", "fake"},
		Group: "vision", Label: "Провайдер",
		Scope: ScopeServer, Overridable: false},
	{Key: "vision.model", Type: String, Default: "",
		Group: "vision", Label: "Модель", Help: "Для OpenRouter — id модели. По умолчанию — из env VLM_MODEL",
		Scope: ScopeServer, Overridable: false},
	{Key: "vision.fallback_models", Type: StringList, Default: []string{},
		Group: "vision", Label: "Запасные модели",
		Scope: ScopeServer, Overridable: false},
	{Key: "vision.prompt_version", Type: String, Default: "v1",
		Group: "vision", Label: "Версия промпта",
		Scope: ScopeServer, Overridable: false},
	{Key: "vision.classify_keyframes_idle", Type: Bool, Default: true,
		Group: "vision", Label: "Проверять плановые кадры без визита", Help: "Выключить дешевле, но тогда вид камеры проверяется только по кадрам изменений",
		Scope: ScopeServer, Overridable: true},
	{Key: "vision.crop_margin", Type: Float, Default: 0.15, Min: f(0), Max: f(0.5), Step: f(0.01),
		Group: "vision", Label: "Запас вокруг зоны для модели",
		Scope: ScopeServer, Overridable: true},
	{Key: "vision.max_side", Type: Int, Default: 1024, Min: f(512), Max: f(2048), Step: f(64), Unit: "px",
		Group: "vision", Label: "Размер изображения для модели",
		Scope: ScopeServer, Overridable: false},
	{Key: "vision.workers", Type: Int, Default: 2, Min: f(1), Max: f(16), Step: f(1),
		Group: "vision", Label: "Параллельных вызовов",
		Scope: ScopeServer, Overridable: false},
	{Key: "vision.max_attempts", Type: Int, Default: 3, Min: f(1), Max: f(10), Step: f(1),
		Group: "vision", Label: "Попыток при ошибке",
		Scope: ScopeServer, Overridable: false},
	{Key: "vision.timeout_s", Type: Int, Default: 45, Min: f(10), Max: f(180), Step: f(5), Unit: "с",
		Group: "vision", Label: "Таймаут вызова",
		Scope: ScopeServer, Overridable: false},

	// Уведомления
	{Key: "notify.telegram.enabled", Type: Bool, Default: false,
		Group: "notify", Label: "Отправлять в Telegram",
		Scope: ScopeServer, Overridable: false},
	{Key: "notify.telegram.chat_ids", Type: StringList, Default: []string{},
		Group: "notify", Label: "Чаты Telegram", Help: "chat_id через запятую",
		Scope: ScopeServer, Overridable: false},
	{Key: "notify.telegram.types", Type: EnumList,
		Default: []string{"VISIT_ARRIVED", "VISIT_LEFT", "OFFLINE", "POWER_OFF", "THEFT_SUSPECTED"},
		Options: []string{"VISIT_ARRIVED", "VISIT_UNLOADING", "VISIT_LEFT", "OFFLINE", "POWER_OFF", "LOW_BATTERY",
			"OVERHEAT", "CAMERA_STALE", "MOVED", "VIEW_CHANGED", "VIEW_BLOCKED", "THEFT_SUSPECTED", "ISSUE_RESOLVED"},
		Group: "notify", Label: "Какие уведомления отправлять",
		Scope: ScopeServer, Overridable: false},
	{Key: "notify.telegram.send_photos", Type: Bool, Default: true,
		Group: "notify", Label: "Прикладывать кадр",
		Scope: ScopeServer, Overridable: false},

	// Хранение
	{Key: "storage.frame_days", Type: Int, Default: 7, Min: f(1), Max: f(365), Step: f(1), Unit: "дн",
		Group: "storage", Label: "Хранить обычные кадры",
		Scope: ScopeServer, Overridable: false},
	{Key: "storage.visit_frame_days", Type: Int, Default: 90, Min: f(1), Max: f(3650), Step: f(1), Unit: "дн",
		Group: "storage", Label: "Хранить кадры визитов",
		Scope: ScopeServer, Overridable: false},
	{Key: "storage.heartbeat_days", Type: Int, Default: 14, Min: f(1), Max: f(365), Step: f(1), Unit: "дн",
		Group: "storage", Label: "Хранить историю heartbeat",
		Scope: ScopeServer, Overridable: false},
}

var byKey = func() map[string]Def {
	m := make(map[string]Def, len(registry))
	for _, d := range registry {
		m[d.Key] = d
	}
	return m
}()

func All() []Def { return registry }

// SetDefault меняет значение по умолчанию при старте (например, vision.model из env VLM_MODEL).
func SetDefault(key string, v any) {
	for i := range registry {
		if registry[i].Key == key {
			registry[i].Default = v
			byKey[key] = registry[i]
			return
		}
	}
	panic("settings: unknown key " + key)
}

func Lookup(key string) (Def, bool) {
	d, ok := byKey[key]
	return d, ok
}
