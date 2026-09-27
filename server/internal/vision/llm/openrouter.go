package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	Register("openrouter", func(c ProviderConfig) Client { return &OpenRouterClient{cfg: c} })
}

// OpenRouterClient — OpenAI-совместимый Chat Completions OpenRouter (docs/05-backend.md §4.2).
// С другим BaseURL подходит для прямого OpenAI или self-hosted vLLM.
type OpenRouterClient struct {
	cfg ProviderConfig
}

type orContent struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *orImageURL `json:"image_url,omitempty"`
}

type orImageURL struct {
	URL string `json:"url"`
}

type orMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type orRequest struct {
	Model          string      `json:"model"`
	Models         []string    `json:"models,omitempty"`
	Messages       []orMessage `json:"messages"`
	ResponseFormat any         `json:"response_format,omitempty"`
	Provider       any         `json:"provider,omitempty"`
	Temperature    float64     `json:"temperature"`
	MaxTokens      int         `json:"max_tokens,omitempty"`
}

type orResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		Cost             float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *OpenRouterClient) Complete(ctx context.Context, req Request) (Response, error) {
	if c.cfg.APIKey == "" {
		return Response{}, &StatusError{Code: http.StatusUnauthorized, Body: "VLM_API_KEY is not set"}
	}
	if req.Model == "" {
		return Response{}, &StatusError{Code: http.StatusBadRequest, Body: "model is not set (vision.model / VLM_MODEL)"}
	}
	content := make([]orContent, 0, len(req.Parts))
	for _, p := range req.Parts {
		if p.ImageJPEG != nil {
			content = append(content, orContent{Type: "image_url", ImageURL: &orImageURL{
				URL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(p.ImageJPEG),
			}})
		} else {
			content = append(content, orContent{Type: "text", Text: p.Text})
		}
	}
	body := orRequest{
		Model:       req.Model,
		Messages:    []orMessage{{Role: "system", Content: req.System}, {Role: "user", Content: content}},
		Temperature: 0,
		MaxTokens:   req.MaxTokens,
	}
	if len(req.Fallbacks) > 0 {
		body.Models = append([]string{req.Model}, req.Fallbacks...)
	}
	if len(req.JSONSchema) > 0 {
		body.ResponseFormat = map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": req.SchemaName, "strict": true, "schema": req.JSONSchema},
		}
		// Только провайдеры, которые поддерживают response_format.
		body.Provider = map[string]any{"require_parameters": true}
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}

	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return Response{}, err
	}
	hr.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	hr.Header.Set("Content-Type", "application/json")
	if c.cfg.Referer != "" {
		hr.Header.Set("HTTP-Referer", c.cfg.Referer)
	}
	if c.cfg.Title != "" {
		hr.Header.Set("X-Title", c.cfg.Title)
	}

	start := time.Now()
	resp, err := c.cfg.HTTP.Do(hr)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Response{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, &StatusError{Code: resp.StatusCode, Body: truncate(string(raw), 500)}
	}
	var or orResponse
	if err := json.Unmarshal(raw, &or); err != nil {
		return Response{}, fmt.Errorf("decode response: %w", err)
	}
	// OpenRouter может вернуть ошибку провайдера с HTTP 200.
	if or.Error != nil {
		return Response{}, &StatusError{Code: http.StatusBadGateway, Body: or.Error.Message}
	}
	if len(or.Choices) == 0 {
		return Response{}, &StatusError{Code: http.StatusBadGateway, Body: "empty choices"}
	}
	return Response{
		Text:      contentText(or.Choices[0].Message.Content),
		Model:     or.Model,
		TokensIn:  or.Usage.PromptTokens,
		TokensOut: or.Usage.CompletionTokens,
		Cost:      or.Usage.Cost,
		Latency:   time.Since(start),
	}, nil
}

// contentText — content бывает строкой или массивом частей {type:"text", text}.
func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return string(raw)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
