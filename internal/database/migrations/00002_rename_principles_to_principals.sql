-- Fixes the "principles" typo in the table name, its ID sequence, and its
-- constraints. Constraint names vary with the GORM version that created the
-- table (for example "uni_principles_token" or "principles_token_key"), so they
-- are renamed by pattern. Renaming a constraint also renames its index.

-- +goose Up
ALTER TABLE principles RENAME TO principals;
ALTER SEQUENCE principles_id_seq RENAME TO principals_id_seq;

-- +goose StatementBegin
DO $$
DECLARE
    c record;
BEGIN
    FOR c IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'principals'::regclass AND conname LIKE '%principles%'
    LOOP
        EXECUTE format(
            'ALTER TABLE principals RENAME CONSTRAINT %I TO %I',
            c.conname, replace(c.conname, 'principles', 'principals')
        );
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE principals RENAME TO principles;
ALTER SEQUENCE principals_id_seq RENAME TO principles_id_seq;

-- +goose StatementBegin
DO $$
DECLARE
    c record;
BEGIN
    FOR c IN
        SELECT conname FROM pg_constraint
        WHERE conrelid = 'principles'::regclass AND conname LIKE '%principals%'
    LOOP
        EXECUTE format(
            'ALTER TABLE principles RENAME CONSTRAINT %I TO %I',
            c.conname, replace(c.conname, 'principals', 'principles')
        );
    END LOOP;
END
$$;
-- +goose StatementEnd
