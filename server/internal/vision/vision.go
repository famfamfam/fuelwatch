// Package vision — предметный уровень распознавания (D-08): кадр + эталон + зоны → Observation.
// От провайдера не зависит: вызов модели — через llm.Client.
package vision

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"strings"
	"time"

	"fuelwatch/internal/settings"
	"fuelwatch/internal/vision/llm"
	"fuelwatch/internal/zones"
)

type ClassifyInput struct {
	Frame      image.Image // повёрнутый кадр: полный или вырез
	CropRect   *Rect       // где вырез лежит в полном кадре; nil — кадр полный
	Reference  image.Image // эталон (полный), может быть nil
	Zones      []zones.Zone
	DeviceName string
	Keyframe   bool   // полный плановый кадр — дополнительно проверить вид камеры целиком
	Model      string // пусто — vision.model; для проверки на кадре можно указать другую
	// PromptVersion — пусто: vision.prompt_version (replay сравнивает версии).
	PromptVersion string
}

type Observation struct {
	TankerPresent        bool    `json:"tanker_present"`
	Confidence           float64 `json:"confidence"`
	TankerInZone         bool    `json:"tanker_in_zone"`
	ViewMatchesReference bool    `json:"view_matches_reference"`
	ViewObstructed       bool    `json:"view_obstructed"`
	Note                 string  `json:"note"`
}

type CallMeta struct {
	Provider, Model, PromptVersion string
	TokensIn, TokensOut            int
	Cost                           float64
	Latency                        time.Duration
}

type Classifier interface {
	Classify(ctx context.Context, in ClassifyInput) (Observation, CallMeta, error)
}

//go:embed prompts/v1.txt
var promptV1 string

// Prompts — версии промпта (настройка vision.prompt_version). Новая версия = новый файл + строка здесь.
var Prompts = map[string]string{"v1": promptV1}

// Schema — строгая JSON-схема ответа.
var Schema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "tanker_present": {"type": "boolean"},
    "confidence": {"type": "number"},
    "tanker_in_zone": {"type": "boolean"},
    "view_matches_reference": {"type": "boolean"},
    "view_obstructed": {"type": "boolean"},
    "note": {"type": "string"}
  },
  "required": ["tanker_present", "confidence", "tanker_in_zone", "view_matches_reference", "view_obstructed", "note"],
  "additionalProperties": false
}`)

const (
	schemaName = "tanker_observation"
	maxTokens  = 300
	maxNote    = 150
)

// ErrNotConfigured — провайдер/модель не настроены; повторять бессмысленно.
var ErrNotConfigured = errors.New("VLM is not configured")

// VLMClassifier реализует Classifier поверх любого llm.Client. Провайдер и модель берутся из настроек
// при каждом вызове — смена в панели действует на следующий кадр.
type VLMClassifier struct {
	Settings *settings.Service
	Client   func(provider string) (llm.Client, error)
}

func (c *VLMClassifier) Classify(ctx context.Context, in ClassifyInput) (Observation, CallMeta, error) {
	vals, err := c.Settings.Defaults(ctx)
	if err != nil {
		return Observation{}, CallMeta{}, err
	}
	provider := vals.String("vision.provider")
	model := in.Model
	if model == "" {
		model = vals.String("vision.model")
	}
	pv := in.PromptVersion
	if pv == "" {
		pv = vals.String("vision.prompt_version")
	}
	meta := CallMeta{Provider: provider, Model: model, PromptVersion: pv}
	prompt, ok := Prompts[pv]
	if !ok {
		return Observation{}, meta, fmt.Errorf("%w: unknown prompt version %q", ErrNotConfigured, pv)
	}
	client, err := c.Client(provider)
	if err != nil {
		return Observation{}, meta, fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}
	parts, err := BuildParts(in, vals.Float("vision.crop_margin"), vals.Int("vision.max_side"))
	if err != nil {
		return Observation{}, meta, err
	}
	resp, err := client.Complete(ctx, llm.Request{
		Model:      model,
		Fallbacks:  vals.Strings("vision.fallback_models"),
		System:     prompt,
		Parts:      parts,
		JSONSchema: Schema,
		SchemaName: schemaName,
		MaxTokens:  maxTokens,
		Timeout:    vals.Seconds("vision.timeout_s"),
	})
	if resp.Model != "" {
		meta.Model = resp.Model
	}
	meta.TokensIn, meta.TokensOut, meta.Cost, meta.Latency = resp.TokensIn, resp.TokensOut, resp.Cost, resp.Latency
	if err != nil {
		return Observation{}, meta, err
	}
	obs, err := ParseObservation(resp.Text)
	return obs, meta, err
}

// BuildParts готовит изображения (docs/05-backend.md §4.3): вырез вокруг MONITOR, тот же вырез из эталона,
// контуры зон, уменьшение; для планового кадра — ещё полные кадры для проверки вида.
func BuildParts(in ClassifyInput, margin float64, maxSide int) ([]llm.Part, error) {
	view := Rect{0, 0, 1, 1}
	cur := in.Frame
	if in.CropRect != nil {
		view = *in.CropRect
	} else if x0, y0, x1, y1, ok := zones.MonitorBounds(in.Zones, margin); ok {
		view = Rect{x0, y0, x1, y1}
		cur = cropNorm(in.Frame, view)
	}

	curImg := toRGBA(cur)
	drawZones(curImg, in.Zones, view)
	curJPEG, err := encodeJPEG(fit(curImg, maxSide))
	if err != nil {
		return nil, err
	}

	var parts []llm.Part
	if in.Reference != nil {
		refImg := toRGBA(cropNorm(in.Reference, view))
		drawZones(refImg, in.Zones, view)
		refJPEG, err := encodeJPEG(fit(refImg, maxSide))
		if err != nil {
			return nil, err
		}
		parts = append(parts, llm.Part{Text: "REFERENCE (пустая сцена, без бензовоза):"}, llm.Part{ImageJPEG: refJPEG})
	} else {
		parts = append(parts, llm.Part{Text: "REFERENCE: нет (эталон ещё не задан)."})
	}
	parts = append(parts, llm.Part{Text: "CURRENT:"}, llm.Part{ImageJPEG: curJPEG})

	if in.Keyframe && in.CropRect == nil && in.Reference != nil && view != (Rect{0, 0, 1, 1}) {
		fullRef, err := encodeJPEG(fit(in.Reference, fullViewSide))
		if err != nil {
			return nil, err
		}
		fullCur, err := encodeJPEG(fit(in.Frame, fullViewSide))
		if err != nil {
			return nil, err
		}
		parts = append(parts,
			llm.Part{Text: "FULL REFERENCE (весь кадр, только для проверки вида камеры):"}, llm.Part{ImageJPEG: fullRef},
			llm.Part{Text: "FULL CURRENT:"}, llm.Part{ImageJPEG: fullCur})
	}
	q := "Есть ли в зелёной зоне топливный бензовоз? Ответь строго по JSON-схеме."
	if in.DeviceName != "" {
		q = "Объект: " + in.DeviceName + ". " + q
	}
	parts = append(parts, llm.Part{Text: q})
	return parts, nil
}

// ParseObservation разбирает ответ модели. Терпит обёртку ```json … ```.
func ParseObservation(text string) (Observation, error) {
	t := strings.TrimSpace(text)
	t = strings.TrimPrefix(t, "```json")
	t = strings.TrimPrefix(t, "```")
	t = strings.TrimSuffix(t, "```")
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(t)), &raw); err != nil {
		return Observation{}, fmt.Errorf("model answer is not JSON: %w", err)
	}
	for _, k := range []string{"tanker_present", "confidence", "tanker_in_zone"} {
		if _, ok := raw[k]; !ok {
			return Observation{}, fmt.Errorf("model answer lacks %q", k)
		}
	}
	// Поля вида — необязательные для старых промптов: по умолчанию «всё в порядке».
	o := Observation{ViewMatchesReference: true}
	if err := json.Unmarshal([]byte(strings.TrimSpace(t)), &o); err != nil {
		return Observation{}, fmt.Errorf("model answer has wrong types: %w", err)
	}
	o.Confidence = min(1, max(0, o.Confidence))
	if r := []rune(o.Note); len(r) > maxNote {
		o.Note = string(r[:maxNote])
	}
	return o, nil
}
