-- +goose Up
ALTER TABLE oidc_auth_requests
    ADD COLUMN callback_hash TEXT CONSTRAINT uni_oidc_auth_requests_callback_hash UNIQUE;

-- +goose Down
ALTER TABLE oidc_auth_requests DROP COLUMN callback_hash;
