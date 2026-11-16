-- +goose Up
ALTER TABLE env_oauth_flows ADD COLUMN code_verifier_envelope jsonb;
ALTER TABLE env_oauth_flows ADD CONSTRAINT env_oauth_flows_code_verifier_envelope_check CHECK (
    code_verifier_envelope IS NULL OR (
        jsonb_typeof(code_verifier_envelope) = 'object'
        AND code_verifier_envelope ?& ARRAY['ciphertext','nonce','wrapped_dek','format_version','key_provider','key_version']
        AND jsonb_typeof(code_verifier_envelope->'ciphertext') = 'string' AND length(code_verifier_envelope->>'ciphertext') > 0
        AND jsonb_typeof(code_verifier_envelope->'nonce') = 'string' AND length(code_verifier_envelope->>'nonce') > 0
        AND jsonb_typeof(code_verifier_envelope->'wrapped_dek') = 'string' AND length(code_verifier_envelope->>'wrapped_dek') > 0
        AND jsonb_typeof(code_verifier_envelope->'format_version') = 'number' AND code_verifier_envelope->>'format_version' = '1'
        AND jsonb_typeof(code_verifier_envelope->'key_provider') = 'string' AND length(code_verifier_envelope->>'key_provider') > 0
        AND jsonb_typeof(code_verifier_envelope->'key_version') = 'number' AND (code_verifier_envelope->>'key_version')::bigint > 0
    ) IS TRUE
);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'OAuth encrypted storage cannot be downgraded; restore the database and key backup'; END $$;
-- +goose StatementEnd
