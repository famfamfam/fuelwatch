-- +goose Up
-- Подряд идущие ответы VLM о виде камеры (VIEW_CHANGED / VIEW_BLOCKED).
ALTER TABLE devices ADD COLUMN view_streak jsonb NOT NULL DEFAULT '{}';
CREATE INDEX health_issues_device_opened ON health_issues (device_id, opened_at DESC);

-- +goose Down
DROP INDEX health_issues_device_opened;
ALTER TABLE devices DROP COLUMN view_streak;
