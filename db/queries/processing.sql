-- Tahap 4: status pemrosesan dokumen, hasil OCR, dan field terekstrak.

-- name: MarkDocumentQueued :one
-- Guard anti-duplikasi: dokumen yang sedang queued/processing tidak dapat
-- diantrekan lagi. Disaring per klaim.
UPDATE claim_documents
SET processing_status = 'queued',
    processing_error_code = NULL,
    processing_attempts = 0,
    queued_at = now()
WHERE id = $1 AND claim_id = $2
  AND processing_status IN ('uploaded', 'failed', 'completed')
RETURNING *;

-- name: GetDocumentByID :one
-- Hanya untuk worker (sistem); akses pengguna selalu lewat GetClaimDocument.
SELECT * FROM claim_documents WHERE id = $1;

-- name: MarkDocumentProcessing :one
-- 'processing' diizinkan agar job yang diselamatkan setelah worker mati
-- dapat melanjutkan.
UPDATE claim_documents
SET processing_status = 'processing',
    processing_attempts = sqlc.arg(attempt)::int
WHERE id = sqlc.arg(id) AND processing_status IN ('queued', 'processing')
RETURNING *;

-- name: MarkDocumentRetrying :exec
UPDATE claim_documents
SET processing_status = 'queued',
    processing_error_code = sqlc.arg(error_code)::text
WHERE id = sqlc.arg(id) AND processing_status = 'processing';

-- name: MarkDocumentCompleted :exec
UPDATE claim_documents
SET processing_status = 'completed',
    processing_error_code = NULL,
    processed_at = now()
WHERE id = $1 AND processing_status = 'processing';

-- name: MarkDocumentFailed :exec
UPDATE claim_documents
SET processing_status = 'failed',
    processing_error_code = sqlc.arg(error_code)::text,
    processed_at = now()
WHERE id = sqlc.arg(id) AND processing_status IN ('queued', 'processing');

-- name: DeleteOCRResults :exec
DELETE FROM ocr_results WHERE document_id = $1;

-- name: DeleteExtractedFields :exec
DELETE FROM extracted_fields WHERE document_id = $1;

-- name: InsertOCRResult :exec
INSERT INTO ocr_results (document_id, page_number, extracted_text, engine, engine_version,
                         mean_confidence, word_count)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: InsertExtractedField :exec
INSERT INTO extracted_fields (document_id, field_key, field_value, status, confidence,
                              page_number, source_excerpt, extractor_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListOCRResults :many
SELECT * FROM ocr_results WHERE document_id = $1 ORDER BY page_number;

-- name: ListExtractedFields :many
SELECT * FROM extracted_fields WHERE document_id = $1 ORDER BY id;

-- name: ListClaimFormFields :many
-- Field nama & nomor polis dari formulir klaim yang selesai diproses,
-- untuk perbandingan "data terdeteksi vs data klaim".
SELECT f.id, f.document_id, f.field_key, f.field_value, f.status, f.confidence,
       f.page_number, f.source_excerpt
FROM extracted_fields f
JOIN claim_documents d ON d.id = f.document_id
WHERE d.claim_id = $1
  AND d.document_type = 'formulir_klaim'
  AND d.processing_status = 'completed'
  AND f.field_key IN ('claimant_name', 'policy_number')
ORDER BY d.created_at DESC, d.id DESC, f.id;

-- name: GetExtractedFieldForClaim :one
-- Disaring per klaim agar field klaim lain tidak dapat dipakai lewat ID.
SELECT f.id, f.document_id, f.field_key, f.field_value, f.status, f.confidence,
       d.document_type, d.processing_status
FROM extracted_fields f
JOIN claim_documents d ON d.id = f.document_id
WHERE f.id = $1 AND d.claim_id = $2;
