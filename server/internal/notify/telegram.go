package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"fuelwatch/internal/telegram"
)

const (
	telegramAttempts = 3
	telegramTimeout  = 30 * time.Second
)

// ErrTelegramOff — токен бота не задан или нет чатов.
var ErrTelegramOff = errors.New("telegram is not configured")

// sendTelegram — дополнительный канал (D-11): только если notify.telegram.enabled и тип выбран.
// Ошибка Telegram не мешает остальному: статус пишется в notifications.telegram_status.
func (s *Service) sendTelegram(n Notification) {
	ctx, cancel := context.WithTimeout(context.Background(), telegramAttempts*telegramTimeout)
	defer cancel()
	vals, err := s.Settings.Defaults(ctx)
	if err != nil {
		slog.Error("telegram: settings", "err", err)
		return
	}
	if !vals.Bool("notify.telegram.enabled") || !slices.Contains(vals.Strings("notify.telegram.types"), n.Type) {
		return
	}
	chats := telegram.ParseChatIDs(vals.Strings("notify.telegram.chat_ids"))
	if len(chats) == 0 {
		return
	}
	var photo []byte
	if n.FrameID != nil && vals.Bool("notify.telegram.send_photos") {
		photo = s.framePhoto(ctx, *n.FrameID)
	}
	text, kb := s.format(n)

	var ids []int
	var lastErr error
	for _, chat := range chats {
		for attempt := 1; attempt <= telegramAttempts; attempt++ {
			var id int
			if photo != nil {
				id, err = s.Telegram.SendPhoto(ctx, chat, photo, text, kb)
			} else {
				id, err = s.Telegram.SendMessage(ctx, chat, text, kb)
			}
			if err == nil {
				ids = append(ids, id)
				break
			}
			lastErr = err
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}
	status := "sent"
	if len(ids) == 0 {
		status = "failed"
		slog.Warn("telegram: send failed", "notification", n.ID, "err", lastErr)
	}
	idsJSON, _ := json.Marshal(ids)
	if _, err := s.DB.Exec(ctx, `UPDATE notifications SET telegram_status=$2, telegram_message_ids=$3 WHERE id=$1`,
		n.ID, status, idsJSON); err != nil {
		slog.Error("telegram: save status", "err", err)
	}
}

func (s *Service) framePhoto(ctx context.Context, frameID string) []byte {
	var rel string
	if err := s.DB.QueryRow(ctx, `SELECT path FROM frames WHERE id=$1`, frameID).Scan(&rel); err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(s.FramesDir, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	return b
}

// format — те же тексты, что в панели, + ссылка на объект и кнопки.
func (s *Service) format(n Notification) (string, telegram.Keyboard) {
	var b strings.Builder
	b.WriteString(n.Title)
	if n.Body != "" {
		b.WriteString("\n" + n.Body)
	}
	if s.PublicURL != "" && n.DeviceID != nil {
		b.WriteString("\n" + strings.TrimRight(s.PublicURL, "/") + "/devices/" + *n.DeviceID)
	}
	var kb telegram.Keyboard
	if n.VisitID != nil && (n.Type == VisitArrived || n.Type == VisitLeft) {
		row := []telegram.Button{
			{Text: "👍", CallbackData: fmt.Sprintf("fb:up:%d", *n.VisitID)},
			{Text: "👎", CallbackData: fmt.Sprintf("fb:down:%d", *n.VisitID)},
		}
		if n.Type == VisitArrived && n.DeviceID != nil {
			row = append(row, telegram.Button{Text: "📷", CallbackData: "snap:" + *n.DeviceID})
		}
		kb = telegram.Keyboard{row}
	}
	return b.String(), kb
}

// TelegramConfigured — токен задан в env.
func (s *Service) TelegramConfigured() bool { return s.Telegram != nil }

// TestTelegram — тестовое сообщение во все чаты из настроек (даже если канал выключен).
func (s *Service) TestTelegram(ctx context.Context) (int, error) {
	if s.Telegram == nil {
		return 0, ErrTelegramOff
	}
	vals, err := s.Settings.Defaults(ctx)
	if err != nil {
		return 0, err
	}
	chats := telegram.ParseChatIDs(vals.Strings("notify.telegram.chat_ids"))
	if len(chats) == 0 {
		return 0, fmt.Errorf("%w: нет chat_id в настройках", ErrTelegramOff)
	}
	sent := 0
	var lastErr error
	for _, c := range chats {
		if _, err := s.Telegram.SendMessage(ctx, c, "✅ FuelWatch: тестовое сообщение. Уведомления будут приходить сюда.", nil); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	if sent == 0 {
		return 0, lastErr
	}
	return sent, nil
}
