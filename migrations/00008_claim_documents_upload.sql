-- +goose Up
-- Tahap 3: status awal dokumen adalah 'uploaded' (siap untuk OCR di Tahap 4).
ALTER TABLE claim_documents DROP CONSTRAINT IF EXISTS claim_documents_processing_status_check;
ALTER TABLE claim_documents
    ADD CONSTRAINT claim_documents_processing_status_check
    CHECK (processing_status IN ('uploaded', 'queued', 'processing', 'completed', 'failed'));
ALTER TABLE claim_documents ALTER COLUMN processing_status SET DEFAULT 'uploaded';

ALTER TABLE claim_documents
    ADD COLUMN uploaded_by BIGINT REFERENCES users (id) ON DELETE RESTRICT,
    ADD COLUMN page_count  INTEGER CHECK (page_count IS NULL OR page_count > 0),
    -- Dokumen yang diunggah ulang setelah petugas mengonfirmasi duplikat.
    ADD COLUMN duplicate_of BIGINT REFERENCES claim_documents (id) ON DELETE RESTRICT,
    ADD CONSTRAINT claim_documents_document_type_check
        CHECK (document_type IN ('formulir_klaim', 'identitas', 'surat_keterangan_medis', 'kuitansi', 'lainnya'));

CREATE INDEX claim_documents_claim_checksum_idx ON claim_documents (claim_id, checksum);

-- +goose Down
DROP INDEX IF EXISTS claim_documents_claim_checksum_idx;
ALTER TABLE claim_documents
    DROP CONSTRAINT IF EXISTS claim_documents_document_type_check,
    DROP COLUMN IF EXISTS duplicate_of,
    DROP COLUMN IF EXISTS page_count,
    DROP COLUMN IF EXISTS uploaded_by;
UPDATE claim_documents SET processing_status = 'queued' WHERE processing_status = 'uploaded';
ALTER TABLE claim_documents DROP CONSTRAINT IF EXISTS claim_documents_processing_status_check;
ALTER TABLE claim_documents
    ADD CONSTRAINT claim_documents_processing_status_check
    CHECK (processing_status IN ('queued', 'processing', 'completed', 'failed'));
ALTER TABLE claim_documents ALTER COLUMN processing_status SET DEFAULT 'queued';
