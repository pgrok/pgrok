-- +goose Up
CREATE TABLE oidc_keys (
    id         TEXT PRIMARY KEY,
    use        TEXT NOT NULL,
    algorithm  TEXT NOT NULL,
    material   BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_oidc_keys_use ON oidc_keys (use);

CREATE TABLE oidc_auth_requests (
    id                    TEXT PRIMARY KEY,
    client_id             TEXT NOT NULL,
    redirect_uri          TEXT NOT NULL,
    state                 TEXT NOT NULL,
    nonce                 TEXT NOT NULL,
    scopes                TEXT NOT NULL,
    response_type         TEXT NOT NULL,
    response_mode         TEXT NOT NULL,
    code_challenge        TEXT NOT NULL,
    code_challenge_method TEXT NOT NULL,
    prompt                TEXT NOT NULL,
    max_age               BIGINT,
    login_hint            TEXT NOT NULL,
    code_hash             TEXT CONSTRAINT uni_oidc_auth_requests_code_hash UNIQUE,
    principal_id          BIGINT NOT NULL,
    auth_time             TIMESTAMPTZ,
    done                  BOOLEAN NOT NULL,
    expires_at            TIMESTAMPTZ NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_oidc_auth_requests_expires_at ON oidc_auth_requests (expires_at);

CREATE TABLE oidc_access_tokens (
    id               TEXT PRIMARY KEY,
    client_id        TEXT NOT NULL,
    subject          TEXT NOT NULL,
    audience         TEXT NOT NULL,
    scopes           TEXT NOT NULL,
    refresh_token_id TEXT NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_oidc_access_tokens_client_id ON oidc_access_tokens (client_id);
CREATE INDEX idx_oidc_access_tokens_subject ON oidc_access_tokens (subject);
CREATE INDEX idx_oidc_access_tokens_refresh_token_id ON oidc_access_tokens (refresh_token_id);
CREATE INDEX idx_oidc_access_tokens_expires_at ON oidc_access_tokens (expires_at);

CREATE TABLE oidc_refresh_tokens (
    id              TEXT PRIMARY KEY,
    family_id       TEXT NOT NULL,
    client_id       TEXT NOT NULL,
    subject         TEXT NOT NULL,
    audience        TEXT NOT NULL,
    scopes          TEXT NOT NULL,
    amr             TEXT NOT NULL,
    auth_time       TIMESTAMPTZ NOT NULL,
    access_token_id TEXT NOT NULL,
    used            BOOLEAN NOT NULL,
    used_at         TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_oidc_refresh_tokens_family_id ON oidc_refresh_tokens (family_id);
CREATE INDEX idx_oidc_refresh_tokens_client_id ON oidc_refresh_tokens (client_id);
CREATE INDEX idx_oidc_refresh_tokens_subject ON oidc_refresh_tokens (subject);
CREATE INDEX idx_oidc_refresh_tokens_expires_at ON oidc_refresh_tokens (expires_at);

-- +goose Down
DROP TABLE oidc_refresh_tokens;
DROP TABLE oidc_access_tokens;
DROP TABLE oidc_auth_requests;
DROP TABLE oidc_keys;
