-- +goose Up
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_organization_provider_slug_check;
DROP INDEX llm_providers_organization_default_key;
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_organization_workspace_slug_key;
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_organization_workspace_name_key;

CREATE TEMPORARY TABLE llm_provider_slug_changes (
    uuid uuid, organization_uuid uuid, workspace_uuid uuid, old_slug text, new_slug text
) ON COMMIT DROP;

-- +goose StatementBegin
DO $$
DECLARE
    provider record;
    candidate text;
    suffix integer;
BEGIN
    FOR provider IN
        SELECT * FROM (
            SELECT uuid, organization_uuid, workspace_uuid, slug,
                row_number() OVER (PARTITION BY organization_uuid, slug ORDER BY workspace_uuid NULLS FIRST, created_at, uuid) AS position
            FROM llm_providers
        ) ranked WHERE position > 1 ORDER BY organization_uuid, slug, uuid
    LOOP
        suffix := 2;
        LOOP
            candidate := left(provider.slug, 31 - length(suffix::text)) || '-' || suffix;
            EXIT WHEN NOT EXISTS (
                SELECT 1 FROM llm_providers existing
                WHERE existing.organization_uuid = provider.organization_uuid AND existing.slug = candidate
            );
            suffix := suffix + 1;
        END LOOP;
        INSERT INTO llm_provider_slug_changes VALUES
            (provider.uuid, provider.organization_uuid, provider.workspace_uuid, provider.slug, candidate);
        UPDATE llm_providers SET slug = candidate WHERE uuid = provider.uuid;
    END LOOP;
END;
$$;
-- +goose StatementEnd

ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_organization_slug_key UNIQUE (organization_uuid, slug);

CREATE TEMPORARY TABLE llm_provider_model_refs ON COMMIT DROP AS
SELECT changes.organization_uuid, changes.workspace_uuid,
    changes.old_slug || '/' || (model->>'id') AS model_id,
    changes.new_slug || '/' || (model->>'id') AS qualified_id
FROM llm_provider_slug_changes changes
JOIN llm_providers provider ON provider.uuid = changes.uuid
CROSS JOIN LATERAL jsonb_array_elements(provider.models) AS model;

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

-- +goose StatementBegin
CREATE FUNCTION pg_temp.llm_provider_snapshot(snapshot jsonb, workspace uuid)
RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE
    qualified_id text;
    roster_path text;
    path text[];
    entries jsonb;
BEGIN
    IF jsonb_typeof(snapshot) IS DISTINCT FROM 'object' THEN
        RETURN snapshot;
    END IF;

    SELECT ref.qualified_id INTO qualified_id
    FROM llm_provider_model_refs ref
    WHERE ref.workspace_uuid = workspace
    AND ref.model_id = CASE jsonb_typeof(snapshot->'model')
        WHEN 'object' THEN snapshot->'model'->>'id'
        WHEN 'string' THEN snapshot->>'model'
    END;
    IF qualified_id IS NOT NULL THEN
        IF jsonb_typeof(snapshot->'model') = 'object' THEN
            snapshot := jsonb_set(snapshot, '{model,id}', to_jsonb(qualified_id));
        ELSE
            snapshot := jsonb_set(snapshot, '{model}', to_jsonb(qualified_id));
        END IF;
    END IF;

    IF jsonb_typeof(snapshot#>'{multiagent,advisor}') = 'object' THEN
        snapshot := jsonb_set(snapshot, '{multiagent,advisor}',
            pg_temp.llm_provider_snapshot(snapshot#>'{multiagent,advisor}', workspace));
    END IF;
    FOREACH roster_path IN ARRAY ARRAY[
        'multiagent.agents',
        'multiagent.subagents.predefined_agents',
        'multiagent.workflows.predefined_agents'
    ] LOOP
        path := string_to_array(roster_path, '.');
        IF jsonb_typeof(snapshot#>path) = 'array' THEN
            SELECT COALESCE(jsonb_agg(pg_temp.llm_provider_snapshot(entry, workspace)
                ORDER BY ordinal), '[]'::jsonb) INTO entries
            FROM jsonb_array_elements(snapshot#>path) WITH ORDINALITY AS roster(entry, ordinal);
            snapshot := jsonb_set(snapshot, path, entries);
        END IF;
    END LOOP;
    RETURN snapshot;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
DECLARE
    table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['agents', 'agent_versions'] LOOP
        EXECUTE format($query$
            UPDATE %I agent
            SET multiagent = pg_temp.llm_provider_snapshot(
                jsonb_build_object('multiagent', agent.multiagent), agent.workspace_uuid)->'multiagent'
            WHERE agent.multiagent IS NOT NULL
            AND EXISTS (SELECT 1 FROM llm_provider_slug_changes changes
                WHERE changes.workspace_uuid = agent.workspace_uuid)
        $query$, table_name);
    END LOOP;
    FOREACH table_name IN ARRAY ARRAY['sessions', 'session_threads', 'deployments', 'deployment_runs'] LOOP
        EXECUTE format($query$
            UPDATE %I record
            SET agent_snapshot = pg_temp.llm_provider_snapshot(record.agent_snapshot, record.workspace_uuid)
            WHERE EXISTS (SELECT 1 FROM llm_provider_slug_changes changes
                WHERE changes.organization_uuid = record.organization_uuid
                AND changes.workspace_uuid = record.workspace_uuid)
        $query$, table_name);
    END LOOP;
END;
$$;
-- +goose StatementEnd

DROP FUNCTION pg_temp.llm_provider_snapshot(jsonb, uuid);

UPDATE code_sessions code_session
SET model = ref.qualified_id
FROM llm_provider_model_refs ref
WHERE ref.organization_uuid = code_session.organization_uuid
AND ref.workspace_uuid = code_session.workspace_uuid
AND code_session.model = ref.model_id;

UPDATE code_sessions code_session
SET worker_external_metadata = jsonb_set(code_session.worker_external_metadata, '{model}', to_jsonb(ref.qualified_id))
FROM llm_provider_model_refs ref
WHERE ref.organization_uuid = code_session.organization_uuid
AND ref.workspace_uuid = code_session.workspace_uuid
AND jsonb_typeof(code_session.worker_external_metadata) = 'object'
AND code_session.worker_external_metadata->>'model' = ref.model_id;

UPDATE message_batch_requests request
SET params = jsonb_set(request.params, '{model}', to_jsonb(ref.qualified_id))
FROM message_batches batch, llm_provider_model_refs ref
WHERE batch.uuid = request.message_batch_uuid
AND batch.workspace_uuid = request.workspace_uuid
AND ref.organization_uuid = batch.organization_uuid
AND ref.workspace_uuid = request.workspace_uuid
AND request.status IN ('queued', 'in_flight')
AND jsonb_typeof(request.params) = 'object'
AND request.params->>'model' = ref.model_id;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM llm_providers WHERE workspace_uuid IS NULL AND slug <> 'organization')
        OR EXISTS (SELECT 1 FROM llm_providers WHERE workspace_uuid IS NOT NULL AND slug = 'organization') THEN
        RAISE EXCEPTION 'Cannot roll back while organization providers use the multi-provider configuration';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_organization_slug_key;
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_organization_workspace_slug_key UNIQUE (organization_uuid, workspace_uuid, slug);
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_organization_workspace_name_key UNIQUE (organization_uuid, workspace_uuid, name);
CREATE UNIQUE INDEX llm_providers_organization_default_key ON llm_providers (organization_uuid) WHERE workspace_uuid IS NULL;
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_organization_provider_slug_check
    CHECK ((workspace_uuid IS NULL AND slug = 'organization') OR (workspace_uuid IS NOT NULL AND slug <> 'organization'));
