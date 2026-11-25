-- name: InsertAuditLog :one
INSERT INTO audit_logs (user_id, entity_type, entity_id, action, before_json, after_json)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListRecentAuditLogs :many
SELECT * FROM audit_logs
ORDER BY id DESC
LIMIT $1;

-- name: ListAuditLogsByAction :many
SELECT * FROM audit_logs
WHERE action = $1
ORDER BY id DESC
LIMIT $2;
