-- name: GetUserByEmail :one
SELECT * FROM users
WHERE lower(email) = lower(sqlc.arg(email)::text);

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: UpsertUser :one
INSERT INTO users (name, email, password_hash, role, is_active)
VALUES (sqlc.arg(name), lower(sqlc.arg(email)::text), sqlc.arg(password_hash), sqlc.arg(role), TRUE)
ON CONFLICT ((lower(email))) DO UPDATE
SET name          = EXCLUDED.name,
    password_hash = EXCLUDED.password_hash,
    role          = EXCLUDED.role,
    is_active     = TRUE
RETURNING *;

-- name: SetUserActive :exec
UPDATE users SET is_active = $2
WHERE id = $1;

-- name: ListActiveUsersByRole :many
SELECT id, name FROM users
WHERE role = $1 AND is_active
ORDER BY name, id;
