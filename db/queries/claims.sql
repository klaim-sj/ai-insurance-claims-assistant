-- name: NextClaimNumber :one
-- Wajib dipanggil di dalam transaksi pembuatan klaim.
INSERT INTO claim_number_counters (prefix, last_value)
VALUES (sqlc.arg(prefix), 1)
ON CONFLICT (prefix) DO UPDATE
SET last_value = claim_number_counters.last_value + 1
RETURNING last_value;

-- name: InsertClaim :one
INSERT INTO claims (claim_number, policy_number, claimant_name, product_type, received_at, status, assigned_to)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetClaim :one
SELECT * FROM claims
WHERE id = $1;

-- name: GetClaimByNumber :one
SELECT * FROM claims
WHERE claim_number = $1;

-- name: GetClaimForUpdate :one
SELECT * FROM claims
WHERE id = $1
FOR UPDATE;

-- name: UpdateClaimFields :one
UPDATE claims
SET policy_number = $2,
    claimant_name = $3,
    product_type  = $4,
    received_at   = $5,
    assigned_to   = $6
WHERE id = $1
RETURNING *;

-- name: UpdateClaimStatus :one
UPDATE claims
SET status = sqlc.arg(new_status)
WHERE id = sqlc.arg(id) AND status = sqlc.arg(old_status)
RETURNING *;

-- name: CountClaims :one
SELECT count(*) FROM claims
WHERE (sqlc.narg(search)::text IS NULL
       OR claim_number LIKE sqlc.narg(search)::text ESCAPE '\'
       OR policy_number LIKE sqlc.narg(search)::text ESCAPE '\')
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(product_type)::text IS NULL OR product_type = sqlc.narg(product_type)::text)
  AND (sqlc.narg(assigned_to)::bigint IS NULL OR assigned_to = sqlc.narg(assigned_to)::bigint)
  AND (sqlc.narg(received_from)::timestamptz IS NULL OR received_at >= sqlc.narg(received_from)::timestamptz)
  AND (sqlc.narg(received_before)::timestamptz IS NULL OR received_at < sqlc.narg(received_before)::timestamptz);

-- name: ListClaims :many
SELECT c.id, c.claim_number, c.policy_number, c.claimant_name, c.product_type,
       c.received_at, c.status, c.assigned_to, c.updated_at,
       u.name AS assignee_name
FROM claims c
LEFT JOIN users u ON u.id = c.assigned_to
WHERE (sqlc.narg(search)::text IS NULL
       OR c.claim_number LIKE sqlc.narg(search)::text ESCAPE '\'
       OR c.policy_number LIKE sqlc.narg(search)::text ESCAPE '\')
  AND (sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text)
  AND (sqlc.narg(product_type)::text IS NULL OR c.product_type = sqlc.narg(product_type)::text)
  AND (sqlc.narg(assigned_to)::bigint IS NULL OR c.assigned_to = sqlc.narg(assigned_to)::bigint)
  AND (sqlc.narg(received_from)::timestamptz IS NULL OR c.received_at >= sqlc.narg(received_from)::timestamptz)
  AND (sqlc.narg(received_before)::timestamptz IS NULL OR c.received_at < sqlc.narg(received_before)::timestamptz)
ORDER BY c.received_at DESC, c.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: InsertClaimStatusHistory :one
INSERT INTO claim_status_histories (claim_id, old_status, new_status, changed_by, reason)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListClaimStatusHistories :many
SELECT h.id, h.claim_id, h.old_status, h.new_status, h.changed_by, h.reason, h.created_at,
       u.name AS changed_by_name
FROM claim_status_histories h
LEFT JOIN users u ON u.id = h.changed_by
WHERE h.claim_id = $1
ORDER BY h.created_at ASC, h.id ASC;
