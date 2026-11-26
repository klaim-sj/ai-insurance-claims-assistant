-- +goose Up
-- Tahap 5: aturan validasi deterministik dan temuan lintas dokumen.
-- Temuan adalah bahan peninjauan petugas ("perlu ditinjau"), BUKAN
-- keputusan klaim.

CREATE TABLE validation_rules (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    rule_code     TEXT        NOT NULL CHECK (rule_code ~ '^[A-Z_]{3,64}$'),
    name          TEXT        NOT NULL,
    description   TEXT        NOT NULL,
    -- NULL berarti berlaku lintas jenis dokumen.
    document_type TEXT CHECK (document_type IS NULL OR document_type IN
                              ('formulir_klaim', 'identitas', 'surat_keterangan_medis', 'kuitansi', 'lainnya')),
    rule_config   JSONB       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(rule_config) = 'object'),
    version       INTEGER     NOT NULL CHECK (version > 0),
    is_active     BOOLEAN     NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (rule_code, version)
);

-- Hanya satu versi aktif per aturan.
CREATE UNIQUE INDEX validation_rules_one_active_idx ON validation_rules (rule_code) WHERE is_active;

CREATE TABLE validation_findings (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    claim_id      BIGINT      NOT NULL REFERENCES claims (id) ON DELETE RESTRICT,
    rule_id       BIGINT      NOT NULL REFERENCES validation_rules (id) ON DELETE RESTRICT,
    rule_version  INTEGER     NOT NULL CHECK (rule_version > 0),
    finding_code  TEXT        NOT NULL CHECK (finding_code ~ '^[A-Z_]{3,64}$'),
    severity      TEXT        NOT NULL CHECK (severity IN ('low', 'medium', 'high')),
    -- Pesan netral untuk petugas (tanpa NIK utuh).
    message       TEXT        NOT NULL CHECK (char_length(message) BETWEEN 1 AND 500
                                              AND message !~ '(^|[^0-9])[0-9]{16}($|[^0-9])'),
    -- Daftar evidence reference; wajib minimal satu. NIK hanya tersamar.
    evidence_json JSONB       NOT NULL CHECK (jsonb_typeof(evidence_json) = 'array'
                                              AND jsonb_array_length(evidence_json) >= 1
                                              AND evidence_json::text !~ '(^|[^0-9])[0-9]{16}($|[^0-9])'),
    -- Identitas kondisi (kode + sumber yang dibandingkan) untuk idempotensi:
    -- kondisi yang sama tidak pernah menghasilkan dua baris.
    fingerprint   TEXT        NOT NULL CHECK (char_length(fingerprint) BETWEEN 1 AND 300),
    status        TEXT        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Terakhir kali kondisi masih terdeteksi oleh validasi.
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at   TIMESTAMPTZ,
    UNIQUE (claim_id, fingerprint),
    CHECK ((status = 'resolved') = (resolved_at IS NOT NULL))
);

CREATE INDEX validation_findings_claim_status_idx ON validation_findings (claim_id, status);

CREATE TRIGGER validation_findings_set_updated_at
    BEFORE UPDATE ON validation_findings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Seed aturan awal v1. rule_config HARUS sama dengan
-- internal/validation.DefaultRules (dijaga oleh test).
INSERT INTO validation_rules (rule_code, name, description, document_type, rule_config, version) VALUES
('MISSING_DOCUMENT', 'Dokumen wajib belum ada',
 'Dokumen wajib untuk klaim kesehatan belum diunggah.', NULL,
 '{"required_document_types": ["formulir_klaim", "identitas", "surat_keterangan_medis", "kuitansi"]}', 1),
('MISSING_FIELD', 'Field wajib tidak ditemukan',
 'Field wajib pada dokumen tidak ditemukan atau hasil bacanya tidak pasti.', NULL,
 '{"required_fields": {"formulir_klaim": ["claimant_name", "policy_number", "event_date"], "identitas": ["claimant_name", "nik"], "surat_keterangan_medis": ["claimant_name", "event_date"], "kuitansi": ["claimant_name", "document_date", "amount"]}}', 1),
('NAME_MISMATCH', 'Nama berbeda',
 'Nama pada dokumen berbeda dengan dokumen identitas atau data klaim.', NULL,
 '{"titles": ["dr", "drg", "prof", "ir", "h", "hj", "tn", "ny", "nn", "sdr", "sdri", "bpk", "bapak", "ibu", "an", "alm", "almh", "s.kom", "s.h", "s.e", "s.t", "s.pd", "s.ked", "s.kep", "s.farm", "s.sos", "m.m", "m.kes", "m.t", "m.si", "sp.pd", "sp.a", "amd"]}', 1),
('POLICY_MISMATCH', 'Nomor polis berbeda',
 'Nomor polis pada dokumen berbeda dengan nomor polis pada data klaim.', NULL,
 '{"document_types": ["formulir_klaim"]}', 1),
('DATE_INCONSISTENCY', 'Tanggal tidak konsisten',
 'Tanggal kuitansi lebih awal dari tanggal kejadian/masuk, atau tanggal berada di masa depan.', NULL,
 '{"receipt_document_types": ["kuitansi"], "event_document_types": ["formulir_klaim", "surat_keterangan_medis"], "check_future_dates": true}', 1),
('LOW_QUALITY_DOCUMENT', 'Kualitas dokumen rendah',
 'Hasil OCR halaman dokumen memiliki confidence rendah atau hampir tidak ada teks terbaca.', NULL,
 '{"min_mean_confidence": 60, "min_words": 5}', 1);

-- +goose Down
DROP TABLE IF EXISTS validation_findings;
DROP TABLE IF EXISTS validation_rules;
