-- +goose Up
CREATE TEMPORARY TABLE llm_provider_resume_model_refs ON COMMIT DROP AS
WITH catalog AS (
    SELECT provider.organization_uuid, provider.workspace_uuid, model.id AS model_id,
        provider.slug || '/' || model.id AS qualified_id
    FROM llm_providers provider
    CROSS JOIN LATERAL jsonb_array_elements_text(provider.model_ids) AS model(id)
), unique_models AS (
    SELECT organization_uuid, workspace_uuid, model_id, min(qualified_id) AS qualified_id
    FROM catalog
    GROUP BY organization_uuid, workspace_uuid, model_id
    HAVING count(*) = 1
)
SELECT candidate.organization_uuid, candidate.workspace_uuid, candidate.model_id, candidate.qualified_id
FROM unique_models candidate
WHERE NOT EXISTS (
    SELECT 1 FROM catalog qualified
    WHERE qualified.organization_uuid = candidate.organization_uuid
    AND qualified.workspace_uuid = candidate.workspace_uuid
    AND qualified.qualified_id = candidate.model_id
);

UPDATE code_sessions code_session
SET worker_external_metadata = jsonb_set(code_session.worker_external_metadata, '{model}', to_jsonb(ref.qualified_id))
FROM llm_provider_resume_model_refs ref
WHERE ref.organization_uuid = code_session.organization_uuid
AND ref.workspace_uuid = code_session.workspace_uuid
AND jsonb_typeof(code_session.worker_external_metadata) = 'object'
AND code_session.worker_external_metadata->>'model' = ref.model_id;

UPDATE message_batch_requests request
SET params = jsonb_set(request.params, '{model}', to_jsonb(ref.qualified_id))
FROM message_batches batch, llm_provider_resume_model_refs ref
WHERE batch.uuid = request.message_batch_uuid
AND batch.workspace_uuid = request.workspace_uuid
AND ref.organization_uuid = batch.organization_uuid
AND ref.workspace_uuid = request.workspace_uuid
AND request.status IN ('queued', 'in_flight')
AND jsonb_typeof(request.params) = 'object'
AND request.params->>'model' = ref.model_id;

-- +goose Down
SELECT 1;
