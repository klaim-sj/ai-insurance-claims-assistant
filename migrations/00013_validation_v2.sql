-- +goose Up
-- Tahap 5.5: gating confidence. MISSING_FIELD, NAME_MISMATCH, dan
-- POLICY_MISMATCH naik ke v2; aturan baru NEEDS_MANUAL_VERIFICATION untuk
-- nilai yang tidak dapat dinilai otomatis dengan andal (verifikasi manual,
-- bukan indikasi masalah klaim). rule_config HARUS sama dengan
-- internal/validation.DefaultRules (dijaga oleh test).

UPDATE validation_rules SET is_active = false
WHERE rule_code IN ('MISSING_FIELD', 'NAME_MISMATCH', 'POLICY_MISMATCH') AND version = 1;

INSERT INTO validation_rules (rule_code, name, description, document_type, rule_config, version) VALUES
('NEEDS_MANUAL_VERIFICATION', 'Perlu verifikasi manual',
 'Nilai tidak terbaca dengan pasti, dokumen berkualitas rendah, atau perbandingan melibatkan nilai berconfidence rendah; petugas mencocokkan langsung dengan dokumen.', NULL,
 '{"field_severity": "low", "comparison_severity": "medium"}', 1),
('MISSING_FIELD', 'Field wajib tidak ditemukan',
 'Field wajib tidak ditemukan pada dokumen yang kualitas hasil bacanya baik (field tidak pasti atau dokumen kualitas rendah menjadi verifikasi manual).', NULL,
 '{"required_fields": {"formulir_klaim": ["claimant_name", "policy_number", "event_date"], "identitas": ["claimant_name", "nik"], "kuitansi": ["claimant_name", "document_date", "amount"], "surat_keterangan_medis": ["claimant_name", "event_date"]}, "document_quality": {"min_mean_confidence": 60, "min_words": 5}}', 2),
('NAME_MISMATCH', 'Nama berbeda',
 'Nama pada dokumen berbeda dengan dokumen identitas atau data klaim, dan kedua nilai terbaca dengan confidence tinggi.', NULL,
 '{"titles": ["dr", "drg", "prof", "ir", "h", "hj", "tn", "ny", "nn", "sdr", "sdri", "bpk", "bapak", "ibu", "an", "alm", "almh", "s.kom", "s.h", "s.e", "s.t", "s.pd", "s.ked", "s.kep", "s.farm", "s.sos", "m.m", "m.kes", "m.t", "m.si", "sp.pd", "sp.a", "amd"], "min_confidence": 85}', 2),
('POLICY_MISMATCH', 'Nomor polis berbeda',
 'Nomor polis pada dokumen berbeda dengan data klaim dan terbaca dengan confidence tinggi.', NULL,
 '{"document_types": ["formulir_klaim"], "min_confidence": 85}', 2);

-- +goose Down
-- Temuan yang merujuk aturan v2/NMV dihapus agar aturan dapat dihapus
-- (rollback skema; temuan dapat dibuat ulang dengan validasi ulang).
DELETE FROM validation_findings WHERE rule_id IN (
    SELECT id FROM validation_rules
    WHERE rule_code = 'NEEDS_MANUAL_VERIFICATION'
       OR (rule_code IN ('MISSING_FIELD', 'NAME_MISMATCH', 'POLICY_MISMATCH') AND version = 2));
DELETE FROM validation_rules
WHERE rule_code = 'NEEDS_MANUAL_VERIFICATION'
   OR (rule_code IN ('MISSING_FIELD', 'NAME_MISMATCH', 'POLICY_MISMATCH') AND version = 2);
UPDATE validation_rules SET is_active = true
WHERE rule_code IN ('MISSING_FIELD', 'NAME_MISMATCH', 'POLICY_MISMATCH') AND version = 1;
