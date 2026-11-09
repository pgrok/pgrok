-- Adds whether the principal is blocked by an admin.

-- +goose Up
ALTER TABLE principals ADD COLUMN blocked BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE principals DROP COLUMN blocked;
