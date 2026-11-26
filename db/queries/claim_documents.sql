-- name: InsertClaimDocument :one
INSERT INTO claim_documents (claim_id, document_type, original_name, storage_path, mime_type,
                             file_size, checksum, processing_status, uploaded_by, page_count, duplicate_of)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'uploaded', $8, $9, $10)
RETURNING *;

-- name: GetClaimDocument :one
-- Selalu disaring per klaim agar dokumen klaim lain tidak bisa diakses lewat ID.
SELECT * FROM claim_documents
WHERE id = $1 AND claim_id = $2;

-- name: ListClaimDocuments :many
SELECT d.id, d.document_type, d.original_name, d.mime_type, d.file_size, d.page_count,
       d.processing_status, d.duplicate_of, d.created_at, u.name AS uploader_name,
       d.processing_error_code, d.processing_attempts, d.processed_at
FROM claim_documents d
LEFT JOIN users u ON u.id = d.uploaded_by
WHERE d.claim_id = $1
ORDER BY d.created_at DESC, d.id DESC;

-- name: ListClaimDocumentChecksums :many
SELECT id, checksum FROM claim_documents
WHERE claim_id = $1 AND checksum = ANY(sqlc.arg(checksums)::text[])
ORDER BY id;
