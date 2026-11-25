-- +goose Up
CREATE TABLE claim_documents (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    claim_id          BIGINT      NOT NULL REFERENCES claims (id) ON DELETE RESTRICT,
    document_type     TEXT        NOT NULL,
    original_name     TEXT        NOT NULL,
    -- Path relatif di private storage dengan nama file acak (bukan nama asli).
    storage_path      TEXT        NOT NULL UNIQUE,
    mime_type         TEXT        NOT NULL
        CHECK (mime_type IN ('application/pdf', 'image/jpeg', 'image/png')),
    file_size         BIGINT      NOT NULL CHECK (file_size > 0),
    -- SHA-256 heksadesimal, dipakai untuk deteksi duplikasi.
    checksum          TEXT        NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    processing_status TEXT        NOT NULL DEFAULT 'queued'
        CHECK (processing_status IN ('queued', 'processing', 'completed', 'failed')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX claim_documents_claim_id_idx ON claim_documents (claim_id, document_type);
CREATE INDEX claim_documents_checksum_idx ON claim_documents (checksum);
CREATE INDEX claim_documents_processing_status_idx ON claim_documents (processing_status);

CREATE TRIGGER claim_documents_set_updated_at
    BEFORE UPDATE ON claim_documents
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE IF EXISTS claim_documents;
