// Package llm — транспорт к мультимодальной модели (D-08). Предметная логика — в пакете vision.
// Новый провайдер = файл с реализацией Client + Register в init(). Код провайдера не выходит за этот пакет.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Part — часть сообщения пользователя: текст или картинка (по порядку).
type Part struct {
	Text      string
	ImageJPEG []byte
}

type Request struct {
	Model      string
	Fallbacks  []string
	System     string
	Parts      []Part
	JSONSchema json.RawMessage // строгая схема ответа
	SchemaName string
	MaxTokens  int
	Timeout    time.Duration
}

type Response struct {
	Text      string // JSON-текст ответа модели
	Model     string // модель, которая реально ответила
	TokensIn  int
	TokensOut int
	Cost      float64
	Latency   time.Duration
}

type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// StatusError — ответ провайдера с ошибкой HTTP.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string { return fmt.Sprintf("provider HTTP %d: %s", e.Code, e.Body) }

// Retryable — 429 и 5xx имеет смысл повторить; прочие 4xx — нет (docs/05-backend.md §4.2).
func Retryable(err error) bool {
	if se, ok := err.(*StatusError); ok {
		return se.Code == http.StatusTooManyRequests || se.Code >= 500
	}
	return true // сеть, таймаут
}

type ProviderConfig struct {
	BaseURL string
	APIKey  string
	Referer string
	Title   string
	HTTP    *http.Client
}

var (
	mu        sync.RWMutex
	factories = map[string]func(ProviderConfig) Client{}
)

func Register(name string, f func(ProviderConfig) Client) {
	mu.Lock()
	defer mu.Unlock()
	factories[name] = f
}

func New(name string, cfg ProviderConfig) (Client, error) {
	mu.RLock()
	f, ok := factories[name]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown VLM provider %q", name)
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{}
	}
	return f(cfg), nil
}

func Providers() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
