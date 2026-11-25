-- +goose Up
-- Skema sesuai github.com/alexedwards/scs/pgxstore.
CREATE TABLE sessions (
    token  TEXT PRIMARY KEY,
    data   BYTEA       NOT NULL,
    expiry TIMESTAMPTZ NOT NULL
);

CREATE INDEX sessions_expiry_idx ON sessions (expiry);

-- +goose Down
DROP TABLE IF EXISTS sessions;
