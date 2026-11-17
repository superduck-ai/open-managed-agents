-- +goose Up
CREATE TABLE channel_personal_vaults (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    channel_type text NOT NULL,
    app_id text NOT NULL,
    actor_id text NOT NULL,
    vault_uuid uuid NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, channel_type, app_id, actor_id)
);

INSERT INTO channel_personal_vaults (organization_uuid, workspace_uuid, channel_type, app_id, actor_id, vault_uuid)
SELECT organization_uuid, workspace_uuid, metadata->>'channel_type', metadata->>'app_id', metadata->>'actor_id', uuid
FROM vaults
WHERE metadata->>'source' = 'channel-oauth' AND deleted_at IS NULL
AND COALESCE(metadata->>'channel_type', '') <> '' AND COALESCE(metadata->>'app_id', '') <> '' AND COALESCE(metadata->>'actor_id', '') <> ''
AND external_id = 'vlt_channel_' || encode(sha256(
    convert_to(organization_uuid::text, 'UTF8') || decode('00', 'hex') ||
    convert_to(workspace_uuid::text, 'UTF8') || decode('00', 'hex') ||
    convert_to(metadata->>'channel_type', 'UTF8') || decode('00', 'hex') ||
    convert_to(metadata->>'app_id', 'UTF8') || decode('00', 'hex') ||
    convert_to(metadata->>'actor_id', 'UTF8')), 'hex');

UPDATE sessions s SET metadata = s.metadata || jsonb_build_object('personal_vault_id', v.external_id, 'oauth_vault_id', v.external_id, 'credential_binding_mode', 'first_vault')
FROM channel_personal_vaults p JOIN vaults v ON v.uuid = p.vault_uuid
WHERE s.organization_uuid = p.organization_uuid AND s.workspace_uuid = p.workspace_uuid
AND s.metadata->>'source' = p.channel_type AND s.metadata->>'app_id' = p.app_id
AND s.metadata->>'actor_id' = p.actor_id AND s.metadata->>'chat_type' = 'p2p'
AND s.vault_ids ? v.external_id;

-- +goose Down
DROP TABLE channel_personal_vaults;
