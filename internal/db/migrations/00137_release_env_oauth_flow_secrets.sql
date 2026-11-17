-- +goose Up
ALTER TABLE env_oauth_flows
    DROP CONSTRAINT env_oauth_flows_client_snapshot_check,
    ADD CONSTRAINT env_oauth_flows_client_snapshot_check CHECK (
        (provider_key = 'builtin:feishu' AND client_id_envelope IS NULL)
        OR (provider_key <> 'builtin:feishu' AND (
            client_id_envelope IS NOT NULL OR status IN ('completed', 'failed', 'expired')
        ))
    );

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Completed OAuth client snapshots have been released; restore the database backup'; END $$;
-- +goose StatementEnd
