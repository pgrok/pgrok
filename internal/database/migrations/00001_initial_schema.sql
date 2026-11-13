-- The schema that GORM's AutoMigrate created before goose took over. Every
-- statement is idempotent so that existing databases, which have no goose
-- version table yet, adopt goose without changes.

-- +goose Up
CREATE TABLE IF NOT EXISTS principles (
    id            BIGSERIAL PRIMARY KEY,
    identifier    TEXT NOT NULL CONSTRAINT uni_principles_identifier UNIQUE,
    display_name  TEXT NOT NULL,
    token         TEXT NOT NULL CONSTRAINT uni_principles_token UNIQUE,
    subdomain     TEXT NOT NULL CONSTRAINT uni_principles_subdomain UNIQUE,
    last_tcp_port BIGINT,
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

-- Databases created before semi-stable TCP port assignment lack this column.
ALTER TABLE principles ADD COLUMN IF NOT EXISTS last_tcp_port BIGINT;

CREATE TABLE IF NOT EXISTS host_keys (
    algorithm  TEXT NOT NULL CONSTRAINT uni_host_keys_algorithm UNIQUE,
    pem        BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE host_keys;
DROP TABLE principles;
