-- +goose Up
CREATE TABLE observations (
    id             bigserial PRIMARY KEY,
    frame_id       text NOT NULL REFERENCES frames(id) ON DELETE CASCADE,
    device_id      text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    result         jsonb,
    tanker_present boolean,
    confidence     real,
    provider       text NOT NULL,
    model          text NOT NULL,
    prompt_version text NOT NULL,
    tokens_in      int NOT NULL DEFAULT 0,
    tokens_out     int NOT NULL DEFAULT 0,
    cost           double precision NOT NULL DEFAULT 0,
    latency_ms     int NOT NULL DEFAULT 0,
    error          text,
    dry_run        boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX observations_frame ON observations (frame_id, id DESC);
CREATE INDEX observations_created ON observations (created_at);

CREATE TABLE visits (
    id                 bigserial PRIMARY KEY,
    device_id          text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    state              text NOT NULL,                 -- PRESENT | LEFT
    started_at         timestamptz NOT NULL,
    confirmed_at       timestamptz NOT NULL,
    last_seen_at       timestamptz NOT NULL,
    ended_at           timestamptz,
    negatives          int NOT NULL DEFAULT 0,
    unloading_notified boolean NOT NULL DEFAULT false,
    first_frame_id     text,
    last_frame_id      text,
    end_reason         text,                          -- left | max_hours | manual
    feedback           text,                          -- up | down
    feedback_by        bigint REFERENCES users(id) ON DELETE SET NULL,
    feedback_at        timestamptz,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX visits_one_open ON visits (device_id) WHERE state = 'PRESENT';
CREATE INDEX visits_device_started ON visits (device_id, started_at DESC);

ALTER TABLE devices ADD COLUMN pending_positive_frame text;

ALTER TABLE frames ADD COLUMN attempts int NOT NULL DEFAULT 0;
ALTER TABLE frames ADD COLUMN processing_started_at timestamptz;
CREATE INDEX frames_pending ON frames (taken_at) WHERE status = 'pending';

-- +goose Down
DROP INDEX frames_pending;
ALTER TABLE devices DROP COLUMN pending_positive_frame;
ALTER TABLE frames DROP COLUMN processing_started_at, DROP COLUMN attempts;
DROP TABLE visits, observations;
