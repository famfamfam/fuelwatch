package store

import (
	"context"
	"encoding/json"
	"time"
)

type Command struct {
	ID        string          `json:"id"`
	DeviceID  string          `json:"device_id"`
	Type      string          `json:"type"`
	Params    json.RawMessage `json:"params"`
	CreatedBy *string         `json:"created_by"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	SentAt    *time.Time      `json:"sent_at"`
	DoneAt    *time.Time      `json:"done_at"`
	OK        *bool           `json:"ok"`
	Error     *string         `json:"error"`
	Status    string          `json:"status"`
}

// status: pending → sent → done | failed | expired
func (c *Command) fillStatus() {
	switch {
	case c.DoneAt != nil && c.OK != nil && *c.OK:
		c.Status = "done"
	case c.DoneAt != nil && c.Error != nil && *c.Error == "expired":
		c.Status = "expired"
	case c.DoneAt != nil:
		c.Status = "failed"
	case c.SentAt != nil:
		c.Status = "sent"
	default:
		c.Status = "pending"
	}
}

// Event — данные SSE command.updated.
func (c *Command) Event() map[string]any {
	return map[string]any{"id": c.ID, "device_id": c.DeviceID, "type": c.Type, "status": c.Status, "error": c.Error}
}

const commandCols = `c.id, c.device_id, c.type, c.params, u.login, c.created_at, c.expires_at, c.sent_at, c.done_at, c.ok, c.error`

func scanCommand(row interface{ Scan(...any) error }) (*Command, error) {
	var c Command
	var params []byte
	err := row.Scan(&c.ID, &c.DeviceID, &c.Type, &params, &c.CreatedBy, &c.CreatedAt, &c.ExpiresAt, &c.SentAt, &c.DoneAt, &c.OK, &c.Error)
	if err != nil {
		if IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.Params = params
	c.fillStatus()
	return &c, nil
}

func CreateCommand(ctx context.Context, q Q, deviceID, typ string, params any, userID *int64, ttl time.Duration) (*Command, error) {
	if params == nil {
		params = map[string]any{}
	}
	id := "c-" + RandomToken(9)
	_, err := q.Exec(ctx, `INSERT INTO commands (id, device_id, type, params, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, deviceID, typ, params, userID, time.Now().Add(ttl))
	if err != nil {
		return nil, err
	}
	return GetCommand(ctx, q, id)
}

func GetCommand(ctx context.Context, q Q, id string) (*Command, error) {
	return scanCommand(q.QueryRow(ctx, `SELECT `+commandCols+` FROM commands c LEFT JOIN users u ON u.id=c.created_by WHERE c.id=$1`, id))
}

func ListCommands(ctx context.Context, q Q, deviceID string, limit int) ([]*Command, error) {
	rows, err := q.Query(ctx, `SELECT `+commandCols+` FROM commands c LEFT JOIN users u ON u.id=c.created_by
		WHERE c.device_id=$1 ORDER BY c.created_at DESC LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Command{}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PendingCommands — невыполненные и не истёкшие команды устройства. Отмечает их отправленными.
// Возвращает команды и те, что отправлены впервые (для SSE).
func PendingCommands(ctx context.Context, q Q, deviceID string) (all []*Command, newlySent []*Command, err error) {
	rows, err := q.Query(ctx, `SELECT `+commandCols+` FROM commands c LEFT JOIN users u ON u.id=c.created_by
		WHERE c.device_id=$1 AND c.done_at IS NULL AND c.expires_at > now() ORDER BY c.created_at`, deviceID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		c, err := scanCommand(rows)
		if err != nil {
			rows.Close()
			return nil, nil, err
		}
		all = append(all, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for _, c := range all {
		if c.SentAt == nil {
			now := time.Now()
			if _, err := q.Exec(ctx, `UPDATE commands SET sent_at=$2 WHERE id=$1 AND sent_at IS NULL`, c.ID, now); err != nil {
				return nil, nil, err
			}
			c.SentAt = &now
			c.fillStatus()
			newlySent = append(newlySent, c)
		}
	}
	return all, newlySent, nil
}

// CompleteCommand — результат выполнения от телефона. nil, если команда уже завершена или чужая.
func CompleteCommand(ctx context.Context, q Q, deviceID, id string, ok bool, errMsg *string) (*Command, error) {
	tag, err := q.Exec(ctx, `UPDATE commands SET done_at=now(), ok=$3, error=$4
		WHERE id=$1 AND device_id=$2 AND done_at IS NULL`, id, deviceID, ok, errMsg)
	if err != nil || tag.RowsAffected() == 0 {
		return nil, err
	}
	return GetCommand(ctx, q, id)
}
