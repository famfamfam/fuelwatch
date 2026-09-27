package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRouterRequestAndResponse(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &got)
		w.Write([]byte(`{"model":"vendor/model-b","choices":[{"message":{"content":"{\"tanker_present\":true}"}}],
			"usage":{"prompt_tokens":1200,"completion_tokens":40,"cost":0.00123}}`))
	}))
	defer srv.Close()

	c, err := New("openrouter", ProviderConfig{BaseURL: srv.URL, APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Complete(context.Background(), Request{
		Model: "vendor/model-a", Fallbacks: []string{"vendor/model-b"}, System: "sys",
		Parts:      []Part{{Text: "CURRENT:"}, {ImageJPEG: []byte{0xff, 0xd8}}},
		JSONSchema: json.RawMessage(`{"type":"object"}`), SchemaName: "obs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer k" {
		t.Errorf("auth %q", auth)
	}
	if resp.Text != `{"tanker_present":true}` || resp.Model != "vendor/model-b" || resp.TokensIn != 1200 || resp.Cost != 0.00123 {
		t.Errorf("response %+v", resp)
	}
	if got["model"] != "vendor/model-a" || len(got["models"].([]any)) != 2 {
		t.Errorf("model/models: %v %v", got["model"], got["models"])
	}
	if got["provider"].(map[string]any)["require_parameters"] != true {
		t.Error("require_parameters must be set with json_schema")
	}
	rf := got["response_format"].(map[string]any)["json_schema"].(map[string]any)
	if rf["strict"] != true || rf["name"] != "obs" {
		t.Errorf("response_format %v", rf)
	}
	msgs := got["messages"].([]any)
	user := msgs[1].(map[string]any)["content"].([]any)
	img := user[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(img, "data:image/jpeg;base64,/9g") {
		t.Errorf("image url %q", img)
	}
}

func TestOpenRouterErrors(t *testing.T) {
	cases := []struct {
		status    int
		body      string
		retryable bool
	}{
		{429, `{"error":{"message":"rate limited"}}`, true},
		{502, `bad gateway`, true},
		{400, `{"error":{"message":"bad model"}}`, false},
		{200, `{"error":{"message":"provider failed","code":502}}`, true},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		cl, _ := New("openrouter", ProviderConfig{BaseURL: srv.URL, APIKey: "k"})
		_, err := cl.Complete(context.Background(), Request{Model: "m"})
		srv.Close()
		if err == nil {
			t.Errorf("%d: want error", c.status)
			continue
		}
		if Retryable(err) != c.retryable {
			t.Errorf("%d %s: retryable=%v, want %v", c.status, c.body, Retryable(err), c.retryable)
		}
	}
	cl, _ := New("openrouter", ProviderConfig{BaseURL: "http://x", APIKey: ""})
	if _, err := cl.Complete(context.Background(), Request{Model: "m"}); err == nil || Retryable(err) {
		t.Errorf("missing key must be a non-retryable error, got %v", err)
	}
}
