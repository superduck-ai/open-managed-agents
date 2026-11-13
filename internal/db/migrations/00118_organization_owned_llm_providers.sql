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
            UPDATE %I record
            SET model = pg_temp.organization_llm_model_value(record.model, workspace.organization_uuid, record.workspace_uuid),
                multiagent = CASE WHEN record.multiagent IS NULL THEN NULL ELSE
                    pg_temp.organization_llm_snapshot(jsonb_build_object('multiagent', record.multiagent),
                        workspace.organization_uuid, record.workspace_uuid)->'multiagent' END
            FROM workspaces workspace
            WHERE workspace.uuid = record.workspace_uuid
        $query$, table_name);
    END LOOP;
    FOREACH table_name IN ARRAY ARRAY['sessions', 'session_threads', 'deployments', 'deployment_runs'] LOOP
        EXECUTE format($query$
            UPDATE %I record
            SET agent_snapshot = pg_temp.organization_llm_snapshot(
                record.agent_snapshot, record.organization_uuid, record.workspace_uuid)
            WHERE record.agent_snapshot IS NOT NULL
        $query$, table_name);
    END LOOP;
END;
$$;
-- +goose StatementEnd

UPDATE code_sessions record
SET model = pg_temp.organization_llm_model(record.organization_uuid, record.workspace_uuid, record.model),
    worker_external_metadata = CASE WHEN jsonb_typeof(record.worker_external_metadata) = 'object'
        AND record.worker_external_metadata ? 'model' THEN
            jsonb_set(record.worker_external_metadata, '{model}',
                pg_temp.organization_llm_model_value(record.worker_external_metadata->'model', record.organization_uuid, record.workspace_uuid))
        ELSE record.worker_external_metadata END;

UPDATE message_batch_requests request
SET params = jsonb_set(request.params, '{model}',
    pg_temp.organization_llm_model_value(request.params->'model', batch.organization_uuid, request.workspace_uuid))
FROM message_batches batch
WHERE batch.uuid = request.message_batch_uuid
AND batch.workspace_uuid = request.workspace_uuid
AND request.status IN ('queued', 'in_flight')
AND jsonb_typeof(request.params) = 'object'
AND request.params ? 'model';

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
