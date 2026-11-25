-- +goose Up
-- Penghitung nomor klaim per prefix. Nomor diambil dengan
-- INSERT ... ON CONFLICT DO UPDATE ... RETURNING di dalam transaksi pembuatan
-- klaim: baris terkunci sampai commit, sehingga aman dari race condition dan
-- tidak ada nomor yang terlewat bila transaksi dibatalkan.
CREATE TABLE claim_number_counters (
    prefix     TEXT   PRIMARY KEY CHECK (prefix ~ '^[A-Z]+(-[A-Z]+)*$'),
    last_value BIGINT NOT NULL CHECK (last_value >= 0)
);

-- Index untuk pencarian awalan (prefix) nomor klaim/polis dengan LIKE.
CREATE INDEX claims_claim_number_pattern_idx ON claims (claim_number text_pattern_ops);
CREATE INDEX claims_policy_number_pattern_idx ON claims (policy_number text_pattern_ops);

-- +goose Down
DROP INDEX IF EXISTS claims_policy_number_pattern_idx;
DROP INDEX IF EXISTS claims_claim_number_pattern_idx;
DROP TABLE IF EXISTS claim_number_counters;
