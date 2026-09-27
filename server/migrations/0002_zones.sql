-- +goose Up
CREATE TABLE zone_versions (
    device_id          text NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    version            int NOT NULL,
    zones              jsonb NOT NULL,
    reference_frame_id text,
    created_by         bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (device_id, version)
);

-- +goose Down
DROP TABLE zone_versions;
