-- +goose Up
ALTER TABLE llm_providers ALTER COLUMN workspace_uuid DROP NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS llm_providers_organization_default_key
    ON llm_providers (organization_uuid) WHERE workspace_uuid IS NULL;

CREATE TEMPORARY TABLE organization_provider_slug_changes ON COMMIT DROP AS
SELECT provider.uuid, provider.organization_uuid, provider.workspace_uuid,
    provider.slug AS old_slug,
    (SELECT 'organization-' || suffix FROM generate_series(2,
        (SELECT count(*)::integer + 2 FROM llm_providers existing
         WHERE existing.organization_uuid = provider.organization_uuid
         AND existing.workspace_uuid = provider.workspace_uuid)) suffix
     WHERE NOT EXISTS (SELECT 1 FROM llm_providers existing
         WHERE existing.organization_uuid = provider.organization_uuid
         AND existing.workspace_uuid = provider.workspace_uuid
         AND existing.slug = 'organization-' || suffix)
     ORDER BY suffix LIMIT 1) AS new_slug
FROM llm_providers provider
WHERE provider.workspace_uuid IS NOT NULL AND provider.slug = 'organization';

CREATE TEMPORARY TABLE organization_llm_model_refs ON COMMIT DROP AS
SELECT changes.organization_uuid, changes.workspace_uuid,
    changes.old_slug || '/' || model.id AS model_id,
    changes.new_slug || '/' || model.id AS qualified_id
FROM organization_provider_slug_changes changes
JOIN llm_providers provider ON provider.uuid = changes.uuid
CROSS JOIN LATERAL jsonb_array_elements_text(provider.model_ids) AS model(id);

UPDATE llm_providers provider SET slug = changes.new_slug
FROM organization_provider_slug_changes changes WHERE provider.uuid = changes.uuid;

ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_organization_provider_slug_check
    CHECK ((workspace_uuid IS NULL AND slug = 'organization')
        OR (workspace_uuid IS NOT NULL AND slug <> 'organization'));

UPDATE agents agent
SET model = CASE jsonb_typeof(agent.model)
    WHEN 'object' THEN jsonb_set(agent.model, '{id}', to_jsonb(ref.qualified_id))
    ELSE to_jsonb(ref.qualified_id)
END
FROM organization_llm_model_refs ref
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
FROM organization_llm_model_refs ref
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
FROM organization_llm_model_refs ref
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
FROM organization_llm_model_refs ref
WHERE ref.organization_uuid = code_session.organization_uuid
AND ref.workspace_uuid = code_session.workspace_uuid
AND code_session.model = ref.model_id;

UPDATE code_sessions code_session
SET worker_external_metadata = jsonb_set(code_session.worker_external_metadata, '{model}', to_jsonb(ref.qualified_id))
FROM organization_llm_model_refs ref
WHERE ref.organization_uuid = code_session.organization_uuid
AND ref.workspace_uuid = code_session.workspace_uuid
AND jsonb_typeof(code_session.worker_external_metadata) = 'object'
AND code_session.worker_external_metadata->>'model' = ref.model_id;

UPDATE message_batch_requests request
SET params = jsonb_set(request.params, '{model}', to_jsonb(ref.qualified_id))
FROM message_batches batch, organization_llm_model_refs ref
WHERE batch.uuid = request.message_batch_uuid
AND batch.workspace_uuid = request.workspace_uuid
AND ref.organization_uuid = batch.organization_uuid
AND ref.workspace_uuid = request.workspace_uuid
AND request.status IN ('queued', 'in_flight')
AND jsonb_typeof(request.params) = 'object'
AND request.params->>'model' = ref.model_id;

-- +goose Down
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_organization_provider_slug_check;
DROP INDEX llm_providers_organization_default_key;
DELETE FROM llm_providers WHERE workspace_uuid IS NULL;
ALTER TABLE llm_providers ALTER COLUMN workspace_uuid SET NOT NULL;
