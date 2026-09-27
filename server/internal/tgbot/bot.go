// Package tgbot — входящие Telegram (docs/05-backend.md §7): кнопки 👍/👎/📷 и команды /status, /snapshot.
// Принимаются только чаты из notify.telegram.chat_ids.
package tgbot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelwatch/internal/settings"
	"fuelwatch/internal/store"
	"fuelwatch/internal/stream"
	"fuelwatch/internal/telegram"
	"fuelwatch/internal/visits"
)

const (
	pollTimeout  = 25 // секунд long polling
	errorBackoff = 5 * time.Second
)

type Bot struct {
	TG        *telegram.Client
	DB        *pgxpool.Pool
	Settings  *settings.Service
	Visits    *visits.Service
	Hub       *stream.Hub
	FramesDir string

	pending sync.Map // command_id → chat_id: куда отправить снимок
}

func (b *Bot) Run(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		ups, err := b.TG.GetUpdates(ctx, offset, pollTimeout)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("telegram: getUpdates", "err", err)
				time.Sleep(errorBackoff)
			}
			continue
		}
		for _, u := range ups {
			offset = u.UpdateID + 1
			b.handle(ctx, u)
		}
	}
}

func (b *Bot) allowed(ctx context.Context, chat int64) bool {
	vals, err := b.Settings.Defaults(ctx)
	return err == nil && slices.Contains(telegram.ParseChatIDs(vals.Strings("notify.telegram.chat_ids")), chat)
}

func (b *Bot) handle(ctx context.Context, u telegram.Update) {
	if cb := u.CallbackQuery; cb != nil && cb.Message != nil {
		if !b.allowed(ctx, cb.Message.Chat.ID) {
			return
		}
		b.TG.AnswerCallback(ctx, cb.ID, b.callback(ctx, cb.Message.Chat.ID, cb.Data))
		return
	}
	m := u.Message
	if m == nil || !b.allowed(ctx, m.Chat.ID) {
		return
	}
	cmd, arg, _ := strings.Cut(strings.TrimSpace(m.Text), " ")
	cmd, _, _ = strings.Cut(cmd, "@") // /status@FuelWatchBot
	var reply string
	switch cmd {
	case "/status":
		reply = b.status(ctx)
	case "/snapshot":
		reply = b.snapshot(ctx, m.Chat.ID, strings.TrimSpace(arg))
	case "/start", "/help":
		reply = "FuelWatch: /status — состояние телефонов, /snapshot [имя] — свежий снимок."
	default:
		return
	}
	if _, err := b.TG.SendMessage(ctx, m.Chat.ID, reply, nil); err != nil {
		slog.Warn("telegram: reply", "err", err)
	}
}

func (b *Bot) callback(ctx context.Context, chat int64, data string) string {
	parts := strings.Split(data, ":")
	switch {
	case len(parts) == 3 && parts[0] == "fb":
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || (parts[1] != "up" && parts[1] != "down") {
			return "Неизвестная кнопка"
		}
		if ok, err := b.Visits.Feedback(ctx, id, parts[1], nil); err != nil || !ok {
			return "Визит не найден"
		}
		return "Спасибо, оценка сохранена"
	case len(parts) == 2 && parts[0] == "snap":
		return b.snapshot(ctx, chat, parts[1])
	}
	return "Неизвестная кнопка"
}

// snapshot создаёт команду; кадр отправится в чат, когда телефон его пришлёт (OnFrame).
func (b *Bot) snapshot(ctx context.Context, chat int64, who string) string {
	devs, err := store.ListDevices(ctx, b.DB)
	if err != nil {
		return "Ошибка сервера"
	}
	var d *store.Device
	for _, x := range devs {
		if x.DisabledAt != nil || !x.Paired {
			continue
		}
		if who == "" || strings.EqualFold(x.ID, who) || strings.Contains(strings.ToLower(x.Name), strings.ToLower(who)) {
			if d != nil && who == "" {
				return "Телефонов несколько — укажите: /snapshot <имя>"
			}
			if d == nil {
				d = x
			}
		}
	}
	if d == nil {
		return "Телефон не найден"
	}
	vals, err := b.Settings.Defaults(ctx)
	if err != nil {
		return "Ошибка сервера"
	}
	c, err := store.CreateCommand(ctx, b.DB, d.ID, "snapshot", nil, nil, vals.Seconds("net.command_ttl_s"))
	if err != nil {
		return "Ошибка сервера"
	}
	b.Hub.Publish("command.updated", c.Event())
	b.pending.Store(c.ID, chat)
	return fmt.Sprintf("📷 %s: снимок запрошен, придёт в течение ~%d с", d.Name, vals.Int("net.heartbeat_interval_s")+10)
}

// OnFrame — кадр по команде из Telegram отправляется в тот чат, откуда её дали.
func (b *Bot) OnFrame(ctx context.Context, deviceID, frameID string, commandID *string) {
	if commandID == nil {
		return
	}
	v, ok := b.pending.LoadAndDelete(*commandID)
	if !ok {
		return
	}
	var rel, name string
	if err := b.DB.QueryRow(ctx, `SELECT f.path, d.name FROM frames f JOIN devices d ON d.id=f.device_id WHERE f.id=$1`, frameID).
		Scan(&rel, &name); err != nil {
		return
	}
	photo, err := os.ReadFile(filepath.Join(b.FramesDir, filepath.FromSlash(rel)))
	if err != nil {
		return
	}
	if _, err := b.TG.SendPhoto(ctx, v.(int64), photo, "📷 "+name+" · "+time.Now().Format("15:04"), nil); err != nil {
		slog.Warn("telegram: snapshot photo", "err", err)
	}
}

func (b *Bot) status(ctx context.Context) string {
	devs, err := store.ListDevices(ctx, b.DB)
	if err != nil {
		return "Ошибка сервера"
	}
	issues, _ := store.OpenIssues(ctx, b.DB, "")
	var sb strings.Builder
	for _, d := range devs {
		if d.DisabledAt != nil || !d.Paired {
			continue
		}
		s := d.Summary(issues)
		online := "🔴 offline"
		if s.Online {
			online = "🟢 online"
		}
		fmt.Fprintf(&sb, "%s — %s, %s", d.Name, online, map[string]string{"armed": "мониторинг", "paused": "пауза", "setup": "настройка"}[d.Mode])
		if s.Battery != nil {
			fmt.Fprintf(&sb, ", %d%%", int(*s.Battery))
		}
		if s.Charging != nil && !*s.Charging {
			sb.WriteString(" (от батареи)")
		}
		if s.BatteryTempC != nil {
			fmt.Fprintf(&sb, ", %.0f°C", *s.BatteryTempC)
		}
		if len(s.OpenIssues) > 0 {
			sb.WriteString("\n  ⚠ " + strings.Join(s.OpenIssues, ", "))
		}
		var started time.Time
		if err := b.DB.QueryRow(ctx, `SELECT started_at FROM visits WHERE device_id=$1 AND state='PRESENT'`, d.ID).Scan(&started); err == nil {
			fmt.Fprintf(&sb, "\n  ⛽ бензовоз с %s (%d мин)", started.Local().Format("15:04"), int(time.Since(started).Minutes()))
		}
		sb.WriteString("\n")
	}
	if sb.Len() == 0 {
		return "Телефонов нет"
	}
	return sb.String()
}
