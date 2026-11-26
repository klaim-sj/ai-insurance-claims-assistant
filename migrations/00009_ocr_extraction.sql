-- +goose Up
-- Tahap 4: OCR dan ekstraksi field deterministik.

ALTER TABLE claim_documents
    -- Kode error terakhir (mis. OCR_TIMEOUT). Tidak pernah berisi teks OCR,
    -- stderr mentah, atau path file.
    ADD COLUMN processing_error_code TEXT
        CHECK (processing_error_code IS NULL OR processing_error_code ~ '^[A-Z_]{3,64}$'),
    ADD COLUMN processing_attempts   INTEGER NOT NULL DEFAULT 0 CHECK (processing_attempts >= 0),
    ADD COLUMN queued_at             TIMESTAMPTZ,
    ADD COLUMN processed_at          TIMESTAMPTZ;

-- Teks OCR per halaman. Deret 16 digit (NIK) sudah dimasking sebelum
-- disimpan, sehingga NIK utuh tidak pernah ada di tabel ini.
CREATE TABLE ocr_results (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id     BIGINT      NOT NULL REFERENCES claim_documents (id) ON DELETE CASCADE,
    page_number     INTEGER     NOT NULL CHECK (page_number > 0),
    -- Pertahanan berlapis: tidak boleh ada deret 16 digit utuh (NIK).
    extracted_text  TEXT        NOT NULL CHECK (extracted_text !~ '(^|[^0-9])[0-9]{16}($|[^0-9])'),
    -- pdftotext (lapisan teks PDF) | tesseract
    engine          TEXT        NOT NULL CHECK (engine IN ('pdftotext', 'tesseract')),
    engine_version  TEXT        NOT NULL,
    -- Rata-rata confidence kata 0–100 (TSV Tesseract); NULL bila tidak ada kata.
    mean_confidence DOUBLE PRECISION CHECK (mean_confidence IS NULL OR mean_confidence BETWEEN 0 AND 100),
    word_count      INTEGER     NOT NULL DEFAULT 0 CHECK (word_count >= 0),
    processed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, page_number)
);

-- Field hasil ekstraksi deterministik. Satu baris per field_key per dokumen,
-- termasuk field yang tidak ditemukan (status not_found, nilai NULL).
CREATE TABLE extracted_fields (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    document_id       BIGINT      NOT NULL REFERENCES claim_documents (id) ON DELETE CASCADE,
    field_key         TEXT        NOT NULL
        CHECK (field_key IN ('claimant_name', 'policy_number', 'nik', 'event_date', 'document_date', 'amount')),
    -- Nilai ternormalisasi (tanggal YYYY-MM-DD, nominal rupiah bulat).
    -- NIK selalu termasking (0000********0001).
    field_value       TEXT,
    status            TEXT        NOT NULL CHECK (status IN ('found', 'uncertain', 'not_found')),
    -- 0–100; sinyal kualitas pembacaan, bukan probabilitas klaim benar.
    confidence        DOUBLE PRECISION CHECK (confidence IS NULL OR confidence BETWEEN 0 AND 100),
    page_number       INTEGER     CHECK (page_number IS NULL OR page_number > 0),
    source_excerpt    TEXT        CHECK (source_excerpt IS NULL OR (char_length(source_excerpt) <= 200
                                         AND source_excerpt !~ '(^|[^0-9])[0-9]{16}($|[^0-9])')),
    extractor_version TEXT        NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, field_key),
    -- found wajib bernilai; not_found tanpa nilai; uncertain boleh tanpa nilai
    -- (label terbaca tetapi nilainya tidak, atau halaman tidak terbaca).
    CHECK (status <> 'found' OR field_value IS NOT NULL),
    CHECK (status <> 'not_found' OR field_value IS NULL),
    -- Pertahanan berlapis: NIK tidak boleh tersimpan utuh.
    CHECK (field_key <> 'nik' OR field_value IS NULL OR field_value ~ '^[0-9]{4}\*{8}[0-9]{4}$')
);

CREATE INDEX extracted_fields_document_idx ON extracted_fields (document_id);

-- +goose Down
DROP TABLE IF EXISTS extracted_fields;
DROP TABLE IF EXISTS ocr_results;
ALTER TABLE claim_documents
    DROP COLUMN IF EXISTS processed_at,
    DROP COLUMN IF EXISTS queued_at,
    DROP COLUMN IF EXISTS processing_attempts,
    DROP COLUMN IF EXISTS processing_error_code;
