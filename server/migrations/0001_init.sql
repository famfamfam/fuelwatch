-- +goose Up
CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    login         text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    disabled_at   timestamptz
);

-- id = sha256 от значения cookie
CREATE TABLE sessions (
    id         text PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    user_agent text NOT NULL DEFAULT ''
);

CREATE TABLE devices (
    id                     text PRIMARY KEY,
    name                   text NOT NULL,
    token_hash             text UNIQUE,
    mode                   text NOT NULL DEFAULT 'setup',
    live_until             timestamptz,
    last_seen_at           timestamptz,
    last_heartbeat         jsonb,
    camera_caps            jsonb,
    config_version         int NOT NULL DEFAULT 1,
    device_config_version  int NOT NULL DEFAULT 0,
    reference_frame_id     text,
    zones                  jsonb NOT NULL DEFAULT '[]',
    zones_version          int NOT NULL DEFAULT 0,
    settings               jsonb NOT NULL DEFAULT '{}',
    extra_frames_requested int NOT NULL DEFAULT 0,
    pending_positive_count int NOT NULL DEFAULT 0,
    pending_positive_since timestamptz,
    last_frame_id          text,
    last_frame_at          timestamptz,
    app_version            text NOT NULL DEFAULT '',
    model                  text NOT NULL DEFAULT '',
    android                text NOT NULL DEFAULT '',
    created_at             timestamptz NOT NULL DEFAULT now(),
    disabled_at            timestamptz
);

CREATE TABLE pairing_codes (
    code_hash  text PRIMARY KEY,
    device_id  text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

CREATE TABLE settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_by bigint REFERENCES users(id) ON DELETE SET NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- id генерирует телефон (UUID)
CREATE TABLE frames (
    id             text PRIMARY KEY,
    device_id      text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    kind           text NOT NULL,
    taken_at       timestamptz NOT NULL,
    received_at    timestamptz NOT NULL DEFAULT now(),
    path           text NOT NULL,
    thumb_path     text NOT NULL,
    width          int NOT NULL,
    height         int NOT NULL,
    crop_rect      jsonb,
    zoom           real,
    config_version int,
    diff_score     real,
    command_id     text,
    status         text NOT NULL,
    keep_until     timestamptz NOT NULL
);
CREATE INDEX frames_device_taken ON frames (device_id, taken_at DESC);
CREATE INDEX frames_keep_until ON frames (keep_until);

CREATE TABLE events (
    id          text PRIMARY KEY,
    device_id   text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    type        text NOT NULL,
    happened_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    payload     jsonb
);
CREATE INDEX events_device_time ON events (device_id, happened_at DESC);

CREATE TABLE commands (
    id         text PRIMARY KEY,
    device_id  text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    type       text NOT NULL,
    params     jsonb NOT NULL DEFAULT '{}',
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    sent_at    timestamptz,
    done_at    timestamptz,
    ok         boolean,
    error      text
);
CREATE INDEX commands_device_created ON commands (device_id, created_at DESC);
CREATE INDEX commands_pending ON commands (device_id) WHERE done_at IS NULL;

CREATE TABLE heartbeats (
    device_id text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    at        timestamptz NOT NULL,
    body      jsonb NOT NULL
);
CREATE INDEX heartbeats_device_at ON heartbeats (device_id, at);

CREATE TABLE health_issues (
    id        bigserial PRIMARY KEY,
    device_id text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    type      text NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    closed_by bigint REFERENCES users(id) ON DELETE SET NULL,
    details   jsonb
);
CREATE UNIQUE INDEX health_issues_one_open ON health_issues (device_id, type) WHERE closed_at IS NULL;

CREATE TABLE notifications (
    id                   bigserial PRIMARY KEY,
    device_id            text REFERENCES devices(id) ON DELETE CASCADE,
    type                 text NOT NULL,
    severity             text NOT NULL,
    title                text NOT NULL,
    body                 text NOT NULL DEFAULT '',
    frame_id             text,
    visit_id             bigint,
    issue_id             bigint,
    created_at           timestamptz NOT NULL DEFAULT now(),
    read_at              timestamptz,
    read_by              bigint REFERENCES users(id) ON DELETE SET NULL,
    telegram_status      text NOT NULL DEFAULT 'skipped',
    telegram_message_ids jsonb
);
CREATE INDEX notifications_unread ON notifications (id) WHERE read_at IS NULL;
CREATE INDEX notifications_device ON notifications (device_id, id DESC);

-- +goose Down
DROP TABLE notifications, health_issues, heartbeats, commands, events, frames,
           settings, pairing_codes, devices, sessions, users;
