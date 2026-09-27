package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	DB *pgxpool.Pool
}

// Global — глобально заданные значения (без значений по умолчанию).
func (s *Service) Global(ctx context.Context) (map[string]any, error) {
	rows, err := s.DB.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var k string
		var raw []byte
		if err := rows.Scan(&k, &raw); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Effective — итоговые значения для устройства с его переопределениями.
func (s *Service) Effective(ctx context.Context, deviceOverrides map[string]any) (Values, error) {
	g, err := s.Global(ctx)
	if err != nil {
		return nil, err
	}
	v, _ := Resolve(g, deviceOverrides)
	return v, nil
}

// Defaults — только глобальный уровень (для серверных задач без устройства).
func (s *Service) Defaults(ctx context.Context) (Values, error) { return s.Effective(ctx, nil) }

// validate проверяет изменения. nil в changes — сброс значения.
func validate(changes map[string]any, caps *Caps, deviceLevel bool) (map[string]any, map[string]string) {
	clean := map[string]any{}
	errs := map[string]string{}
	for k, v := range changes {
		d, ok := Lookup(k)
		if !ok {
			errs[k] = "неизвестная настройка"
			continue
		}
		if deviceLevel && !d.Overridable {
			errs[k] = "настройка задаётся только глобально"
			continue
		}
		if v == nil {
			clean[k] = nil
			continue
		}
		nv, err := d.Normalize(v, caps)
		if err != nil {
			errs[k] = err.Error()
			continue
		}
		clean[k] = nv
	}
	return clean, errs
}

func touchesDevice(changes map[string]any) bool {
	for k := range changes {
		if d, ok := Lookup(k); ok && d.Scope == ScopeDevice {
			return true
		}
	}
	return false
}

// SetGlobal применяет изменения глобальных значений. Возвращает ошибки по полям (тогда ничего не записано).
func (s *Service) SetGlobal(ctx context.Context, changes map[string]any, userID int64) (map[string]string, error) {
	clean, errs := validate(changes, nil, false)
	if len(errs) > 0 {
		return errs, nil
	}
	cur, err := s.Global(ctx)
	if err != nil {
		return nil, err
	}
	next := maps.Clone(cur)
	for k, v := range clean {
		if v == nil {
			delete(next, k)
		} else {
			next[k] = v
		}
	}
	vals, _ := Resolve(next, nil)
	if errs := CheckRules(vals); len(errs) > 0 {
		return errs, nil
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	for k, v := range clean {
		if v == nil {
			_, err = tx.Exec(ctx, `DELETE FROM settings WHERE key=$1`, k)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO settings (key, value, updated_by, updated_at) VALUES ($1, $2, $3, now())
				ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_by=EXCLUDED.updated_by, updated_at=now()`,
				k, jsonValue(v), userID)
		}
		if err != nil {
			return nil, err
		}
	}
	if touchesDevice(clean) {
		if _, err := tx.Exec(ctx, `UPDATE devices SET config_version = config_version + 1`); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit(ctx)
}

// ForDevice — итоговые значения устройства и источник каждого.
func (s *Service) ForDevice(ctx context.Context, deviceID string) (Values, map[string]string, error) {
	var overrides map[string]any
	err := s.DB.QueryRow(ctx, `SELECT settings FROM devices WHERE id=$1`, deviceID).Scan(&overrides)
	if err != nil {
		return nil, nil, err
	}
	g, err := s.Global(ctx)
	if err != nil {
		return nil, nil, err
	}
	v, src := Resolve(g, overrides)
	return v, src, nil
}

// SetDevice применяет переопределения для устройства.
func (s *Service) SetDevice(ctx context.Context, deviceID string, changes map[string]any) (map[string]string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var overrides map[string]any
	var capsRaw []byte
	err = tx.QueryRow(ctx, `SELECT settings, camera_caps FROM devices WHERE id=$1 FOR UPDATE`, deviceID).Scan(&overrides, &capsRaw)
	if err != nil {
		return nil, err
	}
	clean, errs := validate(changes, ParseCaps(capsRaw), true)
	if len(errs) > 0 {
		return errs, nil
	}
	if overrides == nil {
		overrides = map[string]any{}
	}
	for k, v := range clean {
		if v == nil {
			delete(overrides, k)
		} else {
			overrides[k] = v
		}
	}
	g, err := s.Global(ctx)
	if err != nil {
		return nil, err
	}
	vals, _ := Resolve(g, overrides)
	if errs := CheckRules(vals); len(errs) > 0 {
		return errs, nil
	}
	bump := 0
	if touchesDevice(clean) {
		bump = 1
	}
	_, err = tx.Exec(ctx, `UPDATE devices SET settings=$2, config_version=config_version+$3 WHERE id=$1`,
		deviceID, overrides, bump)
	if err != nil {
		return nil, err
	}
	return nil, tx.Commit(ctx)
}

func jsonValue(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("settings: marshal %T: %v", v, err))
	}
	return b
}
