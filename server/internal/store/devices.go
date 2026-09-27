package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type Device struct {
	ID                   string
	Name                 string
	Mode                 string
	LiveUntil            *time.Time
	LastSeenAt           *time.Time
	LastHeartbeat        []byte
	CameraCaps           []byte
	ConfigVersion        int
	DeviceConfigVersion  int
	ReferenceFrameID     *string
	Zones                []byte
	ZonesVersion         int
	Settings             map[string]any
	ExtraFramesRequested int
	LastFrameID          *string
	LastFrameAt          *time.Time
	AppVersion           string
	Model                string
	Android              string
	Paired               bool
	CreatedAt            time.Time
	DisabledAt           *time.Time
}

const deviceCols = `id, name, mode, live_until, last_seen_at, last_heartbeat, camera_caps,
	config_version, device_config_version, reference_frame_id, zones, zones_version, settings,
	extra_frames_requested, last_frame_id, last_frame_at, app_version, model, android,
	token_hash IS NOT NULL, created_at, disabled_at`

func scanDevice(row pgx.Row) (*Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.Name, &d.Mode, &d.LiveUntil, &d.LastSeenAt, &d.LastHeartbeat, &d.CameraCaps,
		&d.ConfigVersion, &d.DeviceConfigVersion, &d.ReferenceFrameID, &d.Zones, &d.ZonesVersion, &d.Settings,
		&d.ExtraFramesRequested, &d.LastFrameID, &d.LastFrameAt, &d.AppVersion, &d.Model, &d.Android,
		&d.Paired, &d.CreatedAt, &d.DisabledAt)
	if IsNoRows(err) {
		return nil, ErrNotFound
	}
	return &d, err
}

func GetDevice(ctx context.Context, q Q, id string) (*Device, error) {
	return scanDevice(q.QueryRow(ctx, `SELECT `+deviceCols+` FROM devices WHERE id=$1`, id))
}

func DeviceByTokenHash(ctx context.Context, q Q, hash string) (*Device, error) {
	return scanDevice(q.QueryRow(ctx, `SELECT `+deviceCols+` FROM devices WHERE token_hash=$1`, hash))
}

func ListDevices(ctx context.Context, q Q) ([]*Device, error) {
	rows, err := q.Query(ctx, `SELECT `+deviceCols+` FROM devices ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type Issue struct {
	ID       int64           `json:"id"`
	DeviceID string          `json:"device_id"`
	Type     string          `json:"type"`
	OpenedAt time.Time       `json:"opened_at"`
	ClosedAt *time.Time      `json:"closed_at"`
	Details  json.RawMessage `json:"details"`
}

// OpenIssues — открытые проблемы; deviceID="" — по всем устройствам.
func OpenIssues(ctx context.Context, q Q, deviceID string) ([]Issue, error) {
	rows, err := q.Query(ctx, `SELECT id, device_id, type, opened_at, closed_at, COALESCE(details, 'null'::jsonb)
		FROM health_issues WHERE closed_at IS NULL AND ($1 = '' OR device_id = $1) ORDER BY opened_at`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var i Issue
		var details []byte
		if err := rows.Scan(&i.ID, &i.DeviceID, &i.Type, &i.OpenedAt, &i.ClosedAt, &details); err != nil {
			return nil, err
		}
		i.Details = details
		out = append(out, i)
	}
	return out, rows.Err()
}

// Summary — краткий статус устройства: для дашборда и SSE device.status.
type Summary struct {
	ID                  string     `json:"id"`
	DeviceID            string     `json:"device_id"`
	Name                string     `json:"name"`
	Mode                string     `json:"mode"`
	Online              bool       `json:"online"`
	Paired              bool       `json:"paired"`
	Disabled            bool       `json:"disabled"`
	LastSeenAt          *time.Time `json:"last_seen_at"`
	LiveUntil           *time.Time `json:"live_until"`
	Battery             *float64   `json:"battery"`
	Charging            *bool      `json:"charging"`
	BatteryTempC        *float64   `json:"battery_temp_c"`
	ThermalStatus       *int       `json:"thermal_status"`
	Network             *string    `json:"network"`
	LastFrameID         *string    `json:"last_frame_id"`
	LastFrameAt         *time.Time `json:"last_frame_at"`
	LastFrameAgeS       *float64   `json:"last_frame_age_s"`
	LastError           *string    `json:"last_error"`
	ConfigVersionServer int        `json:"config_version_server"`
	ConfigVersionDevice int        `json:"config_version_device"`
	OpenIssues          []string   `json:"open_issues"`
	Visit               *OpenVisit `json:"visit"`
	AppVersion          string     `json:"app_version"`
	Model               string     `json:"model"`
	Android             string     `json:"android"`
}

type heartbeatStatus struct {
	Battery       *float64 `json:"battery"`
	Charging      *bool    `json:"charging"`
	BatteryTempC  *float64 `json:"battery_temp_c"`
	ThermalStatus *int     `json:"thermal_status"`
	Network       *string  `json:"network"`
	LastFrameAgeS *float64 `json:"last_frame_age_s"`
	LastError     *string  `json:"last_error"`
}

func (d *Device) Summary(issues []Issue) Summary {
	s := Summary{
		ID: d.ID, DeviceID: d.ID, Name: d.Name, Mode: d.Mode,
		Paired: d.Paired, Disabled: d.DisabledAt != nil,
		LastSeenAt: d.LastSeenAt, LiveUntil: d.LiveUntil,
		LastFrameID: d.LastFrameID, LastFrameAt: d.LastFrameAt,
		ConfigVersionServer: d.ConfigVersion, ConfigVersionDevice: d.DeviceConfigVersion,
		AppVersion: d.AppVersion, Model: d.Model, Android: d.Android,
		OpenIssues: []string{},
	}
	offline := false
	for _, i := range issues {
		if i.DeviceID == d.ID {
			s.OpenIssues = append(s.OpenIssues, i.Type)
			if i.Type == "OFFLINE" {
				offline = true
			}
		}
	}
	s.Online = d.LastSeenAt != nil && !offline && d.DisabledAt == nil
	if len(d.LastHeartbeat) > 0 {
		var hb heartbeatStatus
		if json.Unmarshal(d.LastHeartbeat, &hb) == nil {
			s.Battery, s.Charging, s.BatteryTempC = hb.Battery, hb.Charging, hb.BatteryTempC
			s.ThermalStatus, s.Network, s.LastFrameAgeS, s.LastError = hb.ThermalStatus, hb.Network, hb.LastFrameAgeS, hb.LastError
		}
	}
	return s
}

// OpenVisit — текущий визит бензовоза.
type OpenVisit struct {
	ID         int64     `json:"id"`
	StartedAt  time.Time `json:"started_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// OpenVisits — открытые визиты по устройствам; deviceID="" — по всем.
func OpenVisits(ctx context.Context, q Q, deviceID string) (map[string]*OpenVisit, error) {
	rows, err := q.Query(ctx, `SELECT device_id, id, started_at, last_seen_at FROM visits
		WHERE state='PRESENT' AND ($1 = '' OR device_id = $1)`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*OpenVisit{}
	for rows.Next() {
		var dev string
		var v OpenVisit
		if err := rows.Scan(&dev, &v.ID, &v.StartedAt, &v.LastSeenAt); err != nil {
			return nil, err
		}
		out[dev] = &v
	}
	return out, rows.Err()
}

// DeviceSummary загружает устройство с открытыми проблемами и текущим визитом.
func DeviceSummary(ctx context.Context, q Q, id string) (*Summary, error) {
	d, err := GetDevice(ctx, q, id)
	if err != nil {
		return nil, err
	}
	issues, err := OpenIssues(ctx, q, id)
	if err != nil {
		return nil, err
	}
	s := d.Summary(issues)
	vs, err := OpenVisits(ctx, q, id)
	if err != nil {
		return nil, err
	}
	s.Visit = vs[id]
	return &s, nil
}
