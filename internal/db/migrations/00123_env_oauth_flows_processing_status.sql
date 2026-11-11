-- +goose Up
ALTER TABLE env_oauth_flows
    DROP CONSTRAINT env_oauth_flows_status_check,
    ADD CONSTRAINT env_oauth_flows_status_check CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'expired'));

-- +goose Down
ALTER TABLE env_oauth_flows
    DROP CONSTRAINT env_oauth_flows_status_check,
    ADD CONSTRAINT env_oauth_flows_status_check CHECK (status IN ('pending', 'completed', 'failed', 'expired'));
