-- +goose Up
CREATE TABLE claim_status_histories (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    claim_id    BIGINT      NOT NULL REFERENCES claims (id) ON DELETE RESTRICT,
    -- NULL untuk entri pembuatan klaim (belum ada status sebelumnya).
    old_status  TEXT
        CHECK (old_status IN ('draft', 'submitted', 'processing', 'needs_review',
                              'need_documents', 'reviewed', 'closed')),
    new_status  TEXT        NOT NULL
        CHECK (new_status IN ('draft', 'submitted', 'processing', 'needs_review',
                              'need_documents', 'reviewed', 'closed')),
    -- NULL berarti perubahan dilakukan oleh sistem (mis. pemrosesan dokumen).
    changed_by  BIGINT      REFERENCES users (id) ON DELETE RESTRICT,
    reason      TEXT        CHECK (reason IS NULL OR char_length(reason) BETWEEN 1 AND 500),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX claim_status_histories_claim_id_idx ON claim_status_histories (claim_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS claim_status_histories;
