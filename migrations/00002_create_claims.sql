-- +goose Up
CREATE TABLE claims (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    claim_number   TEXT        NOT NULL UNIQUE,
    policy_number  TEXT        NOT NULL,
    claimant_name  TEXT        NOT NULL,
    product_type   TEXT        NOT NULL,
    received_at    TIMESTAMPTZ NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'submitted', 'processing', 'needs_review',
                          'need_documents', 'reviewed', 'closed')),
    assigned_to    BIGINT      REFERENCES users (id) ON DELETE RESTRICT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX claims_policy_number_idx ON claims (policy_number);
CREATE INDEX claims_claimant_name_idx ON claims (lower(claimant_name));
CREATE INDEX claims_product_type_idx ON claims (product_type);
CREATE INDEX claims_status_idx ON claims (status);
CREATE INDEX claims_assigned_to_idx ON claims (assigned_to);
CREATE INDEX claims_received_at_idx ON claims (received_at);

CREATE TRIGGER claims_set_updated_at
    BEFORE UPDATE ON claims
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE IF EXISTS claims;
