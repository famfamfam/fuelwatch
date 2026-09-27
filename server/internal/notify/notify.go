// Package notify — единая точка уведомлений: запись в БД → SSE → Telegram (если включён).
package notify

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fuelwatch/internal/settings"
	"fuelwatch/internal/stream"
	"fuelwatch/internal/telegram"
)

// Типы уведомлений (docs/06-panel.md §8).
const (
	VisitArrived   = "VISIT_ARRIVED"
	VisitUnloading = "VISIT_UNLOADING"
	VisitLeft      = "VISIT_LEFT"
	Offline        = "OFFLINE"
	PowerOff       = "POWER_OFF"
	LowBattery     = "LOW_BATTERY"
	Overheat       = "OVERHEAT"
	CameraStale    = "CAMERA_STALE"
	Moved          = "MOVED"
	ViewChanged    = "VIEW_CHANGED"
	ViewBlocked    = "VIEW_BLOCKED"
	TheftSuspected = "THEFT_SUSPECTED"
	IssueResolved  = "ISSUE_RESOLVED"
)

const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
	SeverityOK       = "ok"
)

func Severity(t string) string {
	switch t {
	case VisitArrived, VisitUnloading, VisitLeft:
		return SeverityInfo
	case Offline, TheftSuspected:
		return SeverityCritical
	case IssueResolved:
		return SeverityOK
	}
	return SeverityWarning
}

// Categories — типы по категориям панели.
var Categories = map[string][]string{
	"visits":   {VisitArrived, VisitUnloading, VisitLeft},
	"health":   {Offline, PowerOff, LowBattery, Overheat, CameraStale, Moved, ViewChanged, ViewBlocked},
	"security": {TheftSuspected},
	"resolved": {IssueResolved},
}

type Notification struct {
	ID             int64      `json:"id"`
	DeviceID       *string    `json:"device_id"`
	Type           string     `json:"type"`
	Severity       string     `json:"severity"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	FrameID        *string    `json:"frame_id"`
	VisitID        *int64     `json:"visit_id"`
	IssueID        *int64     `json:"issue_id"`
	CreatedAt      time.Time  `json:"created_at"`
	ReadAt         *time.Time `json:"read_at"`
	TelegramStatus string     `json:"telegram_status"`
}

const Cols = `id, device_id, type, severity, title, body, frame_id, visit_id, issue_id, created_at, read_at, telegram_status`

type Scanner interface{ Scan(dest ...any) error }

func Scan(row Scanner) (Notification, error) {
	var n Notification
	err := row.Scan(&n.ID, &n.DeviceID, &n.Type, &n.Severity, &n.Title, &n.Body, &n.FrameID, &n.VisitID,
		&n.IssueID, &n.CreatedAt, &n.ReadAt, &n.TelegramStatus)
	return n, err
}

type Service struct {
	DB        *pgxpool.Pool
	Hub       *stream.Hub
	Settings  *settings.Service
	Telegram  *telegram.Client // nil — TELEGRAM_BOT_TOKEN не задан
	FramesDir string
	PublicURL string
}

// Notify записывает уведомление и рассылает его в панель.
func (s *Service) Notify(ctx context.Context, n Notification) (Notification, error) {
	if n.Severity == "" {
		n.Severity = Severity(n.Type)
	}
	row := s.DB.QueryRow(ctx, `INSERT INTO notifications (device_id, type, severity, title, body, frame_id, visit_id, issue_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+Cols,
		n.DeviceID, n.Type, n.Severity, n.Title, n.Body, n.FrameID, n.VisitID, n.IssueID)
	out, err := Scan(row)
	if err != nil {
		return n, err
	}
	s.Hub.Publish("notification.created", out)
	if s.Telegram != nil {
		go s.sendTelegram(out)
	}
	return out, nil
}
