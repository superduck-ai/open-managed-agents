-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION pg_temp.organization_llm_model(organization uuid, workspace uuid, model_id text)
RETURNS text LANGUAGE plpgsql AS $$
DECLARE
    candidates text[];
BEGIN
    IF model_id IS NULL THEN
        RETURN model_id;
    END IF;
    IF EXISTS (
        SELECT 1 FROM llm_providers provider
        WHERE provider.organization_uuid = organization
        AND (provider.workspace_uuid = workspace OR provider.workspace_uuid IS NULL)
        AND EXISTS (
            SELECT 1 FROM jsonb_array_elements(provider.models) entry
            WHERE provider.slug || '/' || (entry->>'id') = model_id
        )
    ) THEN
        RETURN model_id;
    END IF;
    SELECT array_agg(provider.slug || '/' || model_id ORDER BY provider.uuid) INTO candidates
    FROM llm_providers provider
    WHERE provider.organization_uuid = organization
    AND provider.workspace_uuid = workspace
    AND EXISTS (
        SELECT 1 FROM jsonb_array_elements(provider.models) entry WHERE entry->>'id' = model_id
    );
    IF cardinality(candidates) = 1 THEN
        RETURN candidates[1];
    ELSIF cardinality(candidates) > 1 THEN
        RETURN model_id;
    END IF;
    SELECT array_agg(provider.slug || '/' || model_id ORDER BY provider.uuid) INTO candidates
    FROM llm_providers provider
    WHERE provider.organization_uuid = organization
    AND provider.workspace_uuid IS NULL
    AND EXISTS (
        SELECT 1 FROM jsonb_array_elements(provider.models) entry WHERE entry->>'id' = model_id
    );
    IF cardinality(candidates) = 1 THEN
        RETURN candidates[1];
    END IF;
    RETURN model_id;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION pg_temp.organization_llm_model_value(value jsonb, organization uuid, workspace uuid)
RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE
    original_id text;
    qualified_id text;
BEGIN
    IF jsonb_typeof(value) = 'string' THEN
        original_id := value #>> '{}';
    ELSIF jsonb_typeof(value) = 'object' AND jsonb_typeof(value->'id') = 'string' THEN
        original_id := value->>'id';
    ELSE
        RETURN value;
    END IF;
    qualified_id := pg_temp.organization_llm_model(organization, workspace, original_id);
    IF qualified_id IS NOT DISTINCT FROM original_id THEN
        RETURN value;
    END IF;
    IF jsonb_typeof(value) = 'object' THEN
        RETURN jsonb_set(value, '{id}', to_jsonb(qualified_id));
    END IF;
    RETURN to_jsonb(qualified_id);
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION pg_temp.organization_llm_snapshot(snapshot jsonb, organization uuid, workspace uuid)
RETURNS jsonb LANGUAGE plpgsql AS $$
DECLARE
    roster_path text;
    path text[];
    entries jsonb;
BEGIN
    IF jsonb_typeof(snapshot) IS DISTINCT FROM 'object' THEN
        RETURN snapshot;
    END IF;
    IF snapshot ? 'model' THEN
        snapshot := jsonb_set(snapshot, '{model}',
            pg_temp.organization_llm_model_value(snapshot->'model', organization, workspace));
    END IF;
    IF jsonb_typeof(snapshot#>'{multiagent,advisor}') = 'object' THEN
        snapshot := jsonb_set(snapshot, '{multiagent,advisor}',
            pg_temp.organization_llm_snapshot(snapshot#>'{multiagent,advisor}', organization, workspace));
    END IF;
    FOREACH roster_path IN ARRAY ARRAY[
        'multiagent.agents',
        'multiagent.subagents.predefined_agents',
        'multiagent.workflows.predefined_agents'
    ] LOOP
        path := string_to_array(roster_path, '.');
        IF jsonb_typeof(snapshot#>path) = 'array' THEN
            SELECT COALESCE(jsonb_agg(pg_temp.organization_llm_snapshot(entry, organization, workspace)
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
            WITH candidates AS MATERIALIZED (
                SELECT record.uuid,
                    pg_temp.organization_llm_model_value(record.model, workspace.organization_uuid, record.workspace_uuid) AS model,
                    CASE WHEN record.multiagent IS NULL THEN NULL ELSE
                        pg_temp.organization_llm_snapshot(jsonb_build_object('multiagent', record.multiagent),
                            workspace.organization_uuid, record.workspace_uuid)->'multiagent' END AS multiagent
                FROM %1$I record JOIN workspaces workspace ON workspace.uuid = record.workspace_uuid
            )
            UPDATE %1$I record SET model = candidates.model, multiagent = candidates.multiagent
            FROM candidates WHERE record.uuid = candidates.uuid
            AND (record.model, record.multiagent) IS DISTINCT FROM (candidates.model, candidates.multiagent)
        $query$, table_name);
    END LOOP;
    FOREACH table_name IN ARRAY ARRAY['sessions', 'session_threads', 'deployments', 'deployment_runs'] LOOP
        EXECUTE format($query$
            WITH candidates AS MATERIALIZED (
                SELECT uuid, pg_temp.organization_llm_snapshot(
                    agent_snapshot, organization_uuid, workspace_uuid) AS agent_snapshot
                FROM %1$I
            )
            UPDATE %1$I record SET agent_snapshot = candidates.agent_snapshot
            FROM candidates WHERE record.uuid = candidates.uuid
            AND record.agent_snapshot IS DISTINCT FROM candidates.agent_snapshot
        $query$, table_name);
    END LOOP;
END;
$$;
-- +goose StatementEnd

WITH candidates AS MATERIALIZED (
    SELECT uuid,
        pg_temp.organization_llm_model(organization_uuid, workspace_uuid, model) AS model,
        CASE WHEN jsonb_typeof(worker_external_metadata) = 'object'
            AND worker_external_metadata ? 'model' THEN
                jsonb_set(worker_external_metadata, '{model}',
                    pg_temp.organization_llm_model_value(worker_external_metadata->'model', organization_uuid, workspace_uuid))
            ELSE worker_external_metadata END AS worker_external_metadata
    FROM code_sessions
)
UPDATE code_sessions record
SET model = candidates.model, worker_external_metadata = candidates.worker_external_metadata
FROM candidates WHERE record.uuid = candidates.uuid
AND (record.model, record.worker_external_metadata) IS DISTINCT FROM (candidates.model, candidates.worker_external_metadata);

WITH candidates AS MATERIALIZED (
    SELECT request.uuid, jsonb_set(request.params, '{model}',
        pg_temp.organization_llm_model_value(request.params->'model', batch.organization_uuid, request.workspace_uuid)) AS params
    FROM message_batch_requests request
    JOIN message_batches batch ON batch.uuid = request.message_batch_uuid
        AND batch.workspace_uuid = request.workspace_uuid
    WHERE request.status IN ('queued', 'in_flight')
    AND jsonb_typeof(request.params) = 'object'
    AND request.params ? 'model'
)
UPDATE message_batch_requests request SET params = candidates.params
FROM candidates WHERE request.uuid = candidates.uuid
AND request.params IS DISTINCT FROM candidates.params;

DROP FUNCTION pg_temp.organization_llm_snapshot(jsonb, uuid, uuid);
DROP FUNCTION pg_temp.organization_llm_model_value(jsonb, uuid, uuid);
DROP FUNCTION pg_temp.organization_llm_model(uuid, uuid, text);

DROP INDEX llm_providers_organization_workspace_created_idx;
ALTER TABLE llm_providers RENAME COLUMN workspace_uuid TO secret_workspace_uuid;
ALTER TABLE llm_providers ADD COLUMN created_by_account_uuid uuid;
CREATE INDEX llm_providers_organization_created_idx ON llm_providers (organization_uuid, created_at, uuid);

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'Organization-owned LLM providers cannot restore workspace visibility or creator ownership safely; restore a pre-migration backup with its matching application version';
END;
$$;
-- +goose StatementEnd
