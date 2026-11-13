-- +goose Up
CREATE TABLE oidc_device_authorizations (
    id             TEXT PRIMARY KEY,
    user_code      TEXT NOT NULL CONSTRAINT uni_oidc_device_authorizations_user_code UNIQUE,
    client_id      TEXT NOT NULL,
    scopes         TEXT NOT NULL,
    subject        TEXT NOT NULL,
    auth_time      TIMESTAMPTZ,
    done           BOOLEAN NOT NULL,
    denied         BOOLEAN NOT NULL,
    last_polled_at TIMESTAMPTZ,
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_oidc_device_authorizations_expires_at ON oidc_device_authorizations (expires_at);

-- +goose Down
DROP TABLE oidc_device_authorizations;
