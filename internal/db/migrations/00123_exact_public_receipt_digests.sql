-- +goose Up
ALTER TABLE dsh_public_receipts
    DROP CONSTRAINT dsh_public_receipts_payload_digest_check,
    ADD CONSTRAINT dsh_public_receipts_payload_digest_check
        CHECK (payload_digest ~ '^(jcs-sha256-v1|exact-json-sha256-v1):[0-9a-f]{64}$');

-- +goose Down
ALTER TABLE dsh_public_receipts
    DROP CONSTRAINT dsh_public_receipts_payload_digest_check,
    ADD CONSTRAINT dsh_public_receipts_payload_digest_check
        CHECK (payload_digest ~ '^jcs-sha256-v1:[0-9a-f]{64}$');
