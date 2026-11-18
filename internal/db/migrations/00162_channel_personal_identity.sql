-- +goose Up
ALTER TABLE identity_actor_links ADD COLUMN personal boolean NOT NULL DEFAULT false;
ALTER TABLE channel_user_vaults ADD COLUMN identity_uuid uuid;

UPDATE identity_actor_links l SET personal = true
FROM identities i
WHERE i.uuid = l.identity_uuid AND i.organization_uuid = l.organization_uuid AND i.workspace_uuid = l.workspace_uuid
  AND i.deleted_at IS NULL AND i.namespace = l.channel_type || ':' || l.app_id AND i.external_subject_id = l.actor_id;

CREATE UNIQUE INDEX identity_actor_links_personal_identity_idx ON identity_actor_links (identity_uuid) WHERE personal;

UPDATE channel_user_vaults v SET identity_uuid = l.identity_uuid
FROM identity_actor_links l
WHERE l.personal AND l.organization_uuid = v.organization_uuid AND l.workspace_uuid = v.workspace_uuid
  AND l.channel_type = v.channel_type AND l.app_id = v.app_id AND l.actor_id = v.actor_id;

-- +goose Down
ALTER TABLE channel_user_vaults DROP COLUMN identity_uuid;
DROP INDEX identity_actor_links_personal_identity_idx;
ALTER TABLE identity_actor_links DROP COLUMN personal;
