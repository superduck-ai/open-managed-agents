-- +goose Up
ALTER TABLE organizations
    ADD COLUMN creation_account_uuid uuid,
    ADD COLUMN creation_idempotency_key text,
    ADD COLUMN creation_request_hash text,
    ADD CONSTRAINT organizations_creation_complete CHECK (
        (creation_account_uuid IS NULL AND creation_idempotency_key IS NULL AND creation_request_hash IS NULL)
        OR (creation_account_uuid IS NOT NULL AND creation_idempotency_key IS NOT NULL AND creation_request_hash IS NOT NULL)
    ),
    ADD CONSTRAINT organizations_creation_unique UNIQUE (creation_account_uuid, creation_idempotency_key);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM platform_admin_create_requests r
        LEFT JOIN organizations o ON o.uuid::text = r.response->>'uuid'
        WHERE o.uuid IS NULL
    ) OR EXISTS (
        SELECT response->>'uuid' FROM platform_admin_create_requests
        GROUP BY response->>'uuid' HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION '无法归并组织创建幂等记录：组织不存在或同一组织存在多个请求';
    END IF;
END;
$$;
-- +goose StatementEnd

UPDATE organizations o SET
    creation_account_uuid = r.account_uuid,
    creation_idempotency_key = r.idempotency_key,
    creation_request_hash = r.request_hash
FROM platform_admin_create_requests r WHERE o.uuid::text = r.response->>'uuid';

DROP TABLE platform_admin_create_requests;

-- +goose Down
CREATE TABLE platform_admin_create_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    account_uuid uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_hash text NOT NULL,
    response jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    UNIQUE (account_uuid, idempotency_key)
);

INSERT INTO platform_admin_create_requests (account_uuid, idempotency_key, request_hash, response, expires_at)
SELECT o.creation_account_uuid, o.creation_idempotency_key, o.creation_request_hash,
    jsonb_build_object('uuid', o.uuid, 'name', o.name,
        'website', COALESCE(o.profile->>'website', ''), 'industry', COALESCE(o.profile->>'industry', ''),
        'version', o.management_version, 'created_at', o.created_at,
        'member_count', (SELECT count(*) FROM users u WHERE u.organization_uuid = o.uuid AND u.deleted_at IS NULL),
        'admin_count', (SELECT count(*) FROM users u WHERE u.organization_uuid = o.uuid AND u.deleted_at IS NULL AND u.role = 'admin'),
        'workspace_count', (SELECT count(*) FROM workspaces w WHERE w.organization_uuid = o.uuid)),
    'infinity'::timestamptz
FROM organizations o WHERE o.creation_account_uuid IS NOT NULL;

ALTER TABLE organizations
    DROP CONSTRAINT organizations_creation_unique,
    DROP CONSTRAINT organizations_creation_complete,
    DROP COLUMN creation_account_uuid,
    DROP COLUMN creation_idempotency_key,
    DROP COLUMN creation_request_hash;
