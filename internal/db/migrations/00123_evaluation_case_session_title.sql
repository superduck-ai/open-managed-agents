-- +goose Up

UPDATE sessions AS session
SET title = item_case.name,
    updated_at = now()
FROM evaluation_experiment_items AS item
JOIN evaluation_cases AS item_case
    ON item_case.uuid = item.case_uuid
    AND item_case.organization_uuid = item.organization_uuid
    AND item_case.workspace_uuid = item.workspace_uuid
WHERE session.organization_uuid = item.organization_uuid
AND session.workspace_uuid = item.workspace_uuid
AND session.external_id = item.session_external_id
AND session.deleted_at IS NULL
AND (session.title IS NULL OR btrim(session.title) = '')
AND btrim(item_case.name) <> ''
AND COALESCE(session.metadata->>'evaluation_role', '') <> 'agent_score';

-- +goose Down

UPDATE sessions AS session
SET title = NULL,
    updated_at = now()
FROM evaluation_experiment_items AS item
JOIN evaluation_cases AS item_case
    ON item_case.uuid = item.case_uuid
    AND item_case.organization_uuid = item.organization_uuid
    AND item_case.workspace_uuid = item.workspace_uuid
WHERE session.organization_uuid = item.organization_uuid
AND session.workspace_uuid = item.workspace_uuid
AND session.external_id = item.session_external_id
AND session.title = item_case.name
AND COALESCE(session.metadata->>'experiment_id', '') <> '';
