-- +goose Up
UPDATE sessions s SET metadata = s.metadata || jsonb_build_object('credential_binding_mode', 'first_vault')
FROM channel_sessions b JOIN channels c ON c.external_id = b.channel_external_id
WHERE b.session_external_id = s.external_id
AND s.organization_uuid = c.organization_uuid AND s.workspace_uuid = c.workspace_uuid
AND s.metadata->>'source' = c.channel_type AND s.metadata->>'app_id' = c.app_id
AND s.metadata->>'chat_type' = 'group'
AND s.metadata->>'credential_binding_mode' IS DISTINCT FROM 'first_vault';

-- +goose Down
SELECT 1;
