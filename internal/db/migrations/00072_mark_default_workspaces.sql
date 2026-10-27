-- +goose Up
ALTER TABLE workspaces ADD COLUMN is_default boolean NOT NULL DEFAULT false;

-- 只标识可唯一确认的默认空间，历史成员与资源关联保持原样。
-- +goose StatementBegin
DO $$
DECLARE ambiguous_organizations text;
BEGIN
    SELECT string_agg(CAST(o.uuid AS text), ', ' ORDER BY o.uuid)
    INTO ambiguous_organizations
    FROM organizations o
    WHERE (SELECT count(*) FROM workspaces w
        WHERE w.organization_uuid = o.uuid
        AND (w.external_id = 'workspace_default' OR lower(w.name) = 'default')) <> 1
    OR EXISTS (SELECT 1 FROM workspaces w
        WHERE w.organization_uuid = o.uuid
        AND (w.external_id = 'workspace_default' OR lower(w.name) = 'default')
        AND w.archived_at IS NOT NULL);
    IF ambiguous_organizations IS NOT NULL THEN
        RAISE EXCEPTION 'Cannot identify active default workspace for organizations: %', ambiguous_organizations;
    END IF;
END $$;
-- +goose StatementEnd

UPDATE workspaces SET is_default = true
WHERE external_id = 'workspace_default' OR lower(name) = 'default';

CREATE UNIQUE INDEX workspaces_organization_default_key
ON workspaces (organization_uuid) WHERE is_default;

-- +goose Down
DROP INDEX workspaces_organization_default_key;
ALTER TABLE workspaces DROP COLUMN is_default;
