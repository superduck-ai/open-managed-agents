-- +goose Up
ALTER TABLE env_oauth_flows ADD COLUMN continuation jsonb;
ALTER TABLE mcp_oauth_flows ADD COLUMN continuation jsonb;
ALTER TABLE env_oauth_flows ADD CONSTRAINT env_oauth_flow_continuation_check
    CHECK (continuation IS NULL OR jsonb_typeof(continuation) = 'object');
ALTER TABLE mcp_oauth_flows ADD CONSTRAINT mcp_oauth_flow_continuation_check
    CHECK (continuation IS NULL OR jsonb_typeof(continuation) = 'object');

ALTER TABLE mcp_oauth_flows DROP CONSTRAINT mcp_oauth_flows_status_check;
ALTER TABLE mcp_oauth_flows ADD CONSTRAINT mcp_oauth_flows_status_check
    CHECK (status IN ('pending', 'processing', 'completed', 'failed'));

-- +goose Down
UPDATE mcp_oauth_flows SET status = 'failed', error_code = 'verification_request_failed' WHERE status = 'processing';
ALTER TABLE mcp_oauth_flows DROP CONSTRAINT mcp_oauth_flows_status_check;
ALTER TABLE mcp_oauth_flows ADD CONSTRAINT mcp_oauth_flows_status_check
    CHECK (status IN ('pending', 'completed', 'failed'));
ALTER TABLE env_oauth_flows DROP COLUMN continuation;
ALTER TABLE mcp_oauth_flows DROP COLUMN continuation;
