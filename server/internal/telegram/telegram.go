// Package telegram — минимальный клиент Telegram Bot API: отправка, кнопки, long polling.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Token   string
	BaseURL string // по умолчанию https://api.telegram.org (для тестов — свой сервер)
	HTTP    *http.Client
}

func New(token, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://api.telegram.org"
	}
	return &Client{Token: token, BaseURL: strings.TrimRight(baseURL, "/"), HTTP: &http.Client{Timeout: 60 * time.Second}}
}

type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

type Keyboard [][]Button

type Chat struct {
	ID int64 `json:"id"`
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
	From      *User  `json:"from"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	Data    string   `json:"data"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

func (c *Client) url(method string) string { return c.BaseURL + "/bot" + c.Token + "/" + method }

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var ar apiResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return fmt.Errorf("telegram HTTP %d: %s", resp.StatusCode, string(raw))
	}
	if !ar.OK {
		return fmt.Errorf("telegram: %s", ar.Description)
	}
	if out != nil {
		return json.Unmarshal(ar.Result, out)
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(method), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, kb Keyboard) (int, error) {
	p := map[string]any{"chat_id": chatID, "text": text, "disable_web_page_preview": true}
	if len(kb) > 0 {
		p["reply_markup"] = map[string]any{"inline_keyboard": kb}
	}
	var m Message
	err := c.call(ctx, "sendMessage", p, &m)
	return m.MessageID, err
}

func (c *Client) SendPhoto(ctx context.Context, chatID int64, jpeg []byte, caption string, kb Keyboard) (int, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	w.WriteField("caption", caption)
	if len(kb) > 0 {
		b, _ := json.Marshal(map[string]any{"inline_keyboard": kb})
		w.WriteField("reply_markup", string(b))
	}
	fw, err := w.CreateFormFile("photo", "frame.jpg")
	if err != nil {
		return 0, err
	}
	fw.Write(jpeg)
	w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("sendPhoto"), &buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	var m Message
	err = c.do(req, &m)
	return m.MessageID, err
}

func (c *Client) AnswerCallback(ctx context.Context, id, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}

// GetUpdates — long polling (webhook не нужен): ждёт до timeout секунд.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	var ups []Update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": timeout, "allowed_updates": []string{"message", "callback_query"},
	}, &ups)
	return ups, err
}

// ParseChatIDs — chat_id из настроек (строки) в числа; некорректные пропускаются.
func ParseChatIDs(ss []string) []int64 {
	var out []int64
	for _, s := range ss {
		if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}
