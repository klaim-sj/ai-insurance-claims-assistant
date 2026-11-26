-- Tahap 5: aturan validasi dan temuan.

-- name: ListActiveValidationRules :many
SELECT * FROM validation_rules WHERE is_active ORDER BY id;

-- name: ListValidationDocuments :many
-- Seluruh dokumen klaim (termasuk yang belum selesai) untuk rule engine.
SELECT id, document_type, processing_status, page_count, created_at
FROM claim_documents
WHERE claim_id = $1
ORDER BY created_at, id;

-- name: CountUnfinishedDocuments :one
-- Dokumen yang belum selesai diproses (validasi menunggu semuanya selesai).
SELECT count(*) FROM claim_documents
WHERE claim_id = $1 AND processing_status IN ('uploaded', 'queued', 'processing');

-- name: ListValidationFields :many
-- Field hasil ekstraksi dokumen klaim yang selesai diproses.
SELECT f.document_id, f.field_key, f.field_value, f.status, f.confidence,
       f.page_number, f.source_excerpt
FROM extracted_fields f
JOIN claim_documents d ON d.id = f.document_id
WHERE d.claim_id = $1 AND d.processing_status = 'completed'
ORDER BY f.document_id, f.id;

-- name: ListValidationPages :many
-- Metadata kualitas halaman OCR (tanpa teks).
SELECT o.document_id, o.page_number, o.engine, o.mean_confidence, o.word_count
FROM ocr_results o
JOIN claim_documents d ON d.id = o.document_id
WHERE d.claim_id = $1 AND d.processing_status = 'completed'
ORDER BY o.document_id, o.page_number;

-- name: ListClaimFindingsForUpdate :many
SELECT * FROM validation_findings WHERE claim_id = $1 ORDER BY id FOR UPDATE;

-- name: InsertFinding :one
INSERT INTO validation_findings (claim_id, rule_id, rule_version, finding_code, severity,
                                 message, evidence_json, fingerprint)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: RefreshFinding :one
-- Kondisi masih ada: perbarui isi (mis. versi aturan/evidence) tanpa
-- membuat baris baru. Temuan resolved dibuka kembali; dismissed tetap.
UPDATE validation_findings
SET rule_id = $2, rule_version = $3, severity = $4, message = $5, evidence_json = $6,
    status = CASE WHEN status = 'resolved' THEN 'open' ELSE status END,
    resolved_at = CASE WHEN status = 'resolved' THEN NULL ELSE resolved_at END,
    last_seen_at = now()
WHERE id = $1
RETURNING *;

-- name: ResolveFinding :exec
UPDATE validation_findings
SET status = 'resolved', resolved_at = now()
WHERE id = $1 AND status = 'open';

-- name: ListClaimFindings :many
SELECT * FROM validation_findings
WHERE claim_id = $1
ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'dismissed' THEN 1 ELSE 2 END,
         CASE severity WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END,
         id;

-- name: CountOpenFindings :one
SELECT count(*) FROM validation_findings WHERE claim_id = $1 AND status = 'open';
