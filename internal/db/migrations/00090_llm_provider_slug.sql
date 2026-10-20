-- +goose Up
ALTER TABLE llm_providers ADD COLUMN slug text;

UPDATE llm_providers provider
SET slug = CASE WHEN ranked.position = 1 THEN 'default' ELSE 'default-' || ranked.position END
FROM (
    SELECT uuid,
        row_number() OVER (
            PARTITION BY organization_uuid, workspace_uuid
            ORDER BY created_at, uuid
        ) AS position
    FROM llm_providers
) ranked
WHERE ranked.uuid = provider.uuid;

ALTER TABLE llm_providers ALTER COLUMN slug SET NOT NULL;
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_slug_format
    CHECK (slug ~ '^[a-z0-9][a-z0-9._-]{0,31}$');
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_organization_workspace_slug_key
    UNIQUE (organization_uuid, workspace_uuid, slug);

CREATE TEMPORARY TABLE llm_provider_model_refs ON COMMIT DROP AS
SELECT provider.organization_uuid,
    provider.workspace_uuid,
    model.id AS model_id,
    min(provider.slug) || '/' || model.id AS qualified_id
FROM llm_providers provider
CROSS JOIN LATERAL jsonb_array_elements_text(provider.model_ids) AS model(id)
GROUP BY provider.organization_uuid, provider.workspace_uuid, model.id
HAVING count(*) = 1;

UPDATE agents agent
SET model = CASE jsonb_typeof(agent.model)
    WHEN 'object' THEN jsonb_set(agent.model, '{id}', to_jsonb(ref.qualified_id))
    ELSE to_jsonb(ref.qualified_id)
END
FROM llm_provider_model_refs ref
WHERE ref.workspace_uuid = agent.workspace_uuid
AND (
    (jsonb_typeof(agent.model) = 'object' AND agent.model->>'id' = ref.model_id)
    OR (jsonb_typeof(agent.model) = 'string' AND agent.model #>> '{}' = ref.model_id)
);

UPDATE agent_versions version
SET model = CASE jsonb_typeof(version.model)
    WHEN 'object' THEN jsonb_set(version.model, '{id}', to_jsonb(ref.qualified_id))
    ELSE to_jsonb(ref.qualified_id)
END
FROM llm_provider_model_refs ref
WHERE ref.workspace_uuid = version.workspace_uuid
AND (
    (jsonb_typeof(version.model) = 'object' AND version.model->>'id' = ref.model_id)
    OR (jsonb_typeof(version.model) = 'string' AND version.model #>> '{}' = ref.model_id)
);

UPDATE sessions session
SET agent_snapshot = CASE jsonb_typeof(session.agent_snapshot->'model')
    WHEN 'object' THEN jsonb_set(session.agent_snapshot, '{model,id}', to_jsonb(ref.qualified_id))
    ELSE jsonb_set(session.agent_snapshot, '{model}', to_jsonb(ref.qualified_id))
END
FROM llm_provider_model_refs ref
WHERE ref.organization_uuid = session.organization_uuid
AND ref.workspace_uuid = session.workspace_uuid
AND jsonb_typeof(session.agent_snapshot) = 'object'
AND (
    (jsonb_typeof(session.agent_snapshot->'model') = 'object'
        AND session.agent_snapshot->'model'->>'id' = ref.model_id)
    OR (jsonb_typeof(session.agent_snapshot->'model') = 'string'
        AND session.agent_snapshot->>'model' = ref.model_id)
);

UPDATE code_sessions code_session
SET model = ref.qualified_id
FROM llm_provider_model_refs ref
WHERE ref.organization_uuid = code_session.organization_uuid
AND ref.workspace_uuid = code_session.workspace_uuid
AND code_session.model = ref.model_id;

-- +goose Down
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_organization_workspace_slug_key;
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_slug_format;
ALTER TABLE llm_providers DROP COLUMN slug;
