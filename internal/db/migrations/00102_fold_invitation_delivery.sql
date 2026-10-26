-- +goose Up
ALTER TABLE organization_invites ADD COLUMN email_delivery_status text NOT NULL DEFAULT 'not_queued'
    CHECK (email_delivery_status IN ('not_queued', 'pending', 'sending', 'sent', 'failed', 'cancelled'));

UPDATE organization_invites i SET email_delivery_status = d.status
FROM (
    SELECT DISTINCT ON (invitation_uuid) invitation_uuid, organization_uuid, recipient, expires_at, status
    FROM platform_admin_invite_deliveries ORDER BY invitation_uuid, created_at DESC, uuid DESC
) d
WHERE i.uuid = d.invitation_uuid AND i.organization_uuid = d.organization_uuid
    AND i.email = d.recipient AND i.expires_at = d.expires_at;

-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass(format('%I.river_job', current_schema())) IS NOT NULL THEN
        UPDATE river_job j SET args = (j.args - 'delivery_uuid') || jsonb_build_object(
            'invitation_uuid', d.invitation_uuid, 'organization_uuid', d.organization_uuid,
            'recipient', d.recipient, 'invited_at', COALESCE(i.invited_at, d.created_at), 'expires_at', d.expires_at)
        FROM platform_admin_invite_deliveries d LEFT JOIN organization_invites i
            ON i.uuid = d.invitation_uuid AND i.organization_uuid = d.organization_uuid
        WHERE j.kind = 'platform_invitation_delivery' AND j.args->>'delivery_uuid' = d.uuid::text;
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TABLE platform_admin_invite_deliveries;

-- +goose StatementBegin
CREATE FUNCTION reset_invitation_email_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.invited_at IS DISTINCT FROM OLD.invited_at OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
        OR NEW.email IS DISTINCT FROM OLD.email OR NEW.role IS DISTINCT FROM OLD.role THEN
        NEW.email_delivery_status := 'not_queued';
    END IF;
    IF (NEW.status != 'pending' OR NEW.deleted_at IS NOT NULL OR NEW.expires_at <= clock_timestamp())
        AND NEW.email_delivery_status IN ('pending', 'sending', 'failed') THEN
        NEW.email_delivery_status := 'cancelled';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER invitation_email_delivery BEFORE UPDATE OF invited_at, expires_at, email, role, status, deleted_at
    ON organization_invites FOR EACH ROW EXECUTE FUNCTION reset_invitation_email_delivery();

-- +goose Down
DROP TRIGGER invitation_email_delivery ON organization_invites;
DROP FUNCTION reset_invitation_email_delivery();

CREATE TABLE platform_admin_invite_deliveries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    invitation_uuid uuid NOT NULL,
    recipient text NOT NULL,
    expires_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'cancelled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO platform_admin_invite_deliveries (organization_uuid, invitation_uuid, recipient, expires_at, status)
    SELECT organization_uuid, uuid, email, expires_at, email_delivery_status FROM organization_invites
    WHERE email_delivery_status != 'not_queued';

-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass(format('%I.river_job', current_schema())) IS NOT NULL THEN
        UPDATE river_job j SET args = jsonb_build_object('delivery_uuid', d.uuid)
        FROM platform_admin_invite_deliveries d JOIN organization_invites i
            ON i.uuid = d.invitation_uuid AND i.organization_uuid = d.organization_uuid
        WHERE j.kind = 'platform_invitation_delivery' AND j.args->>'invitation_uuid' = d.invitation_uuid::text
            AND j.args->>'organization_uuid' = d.organization_uuid::text AND j.args->>'recipient' = d.recipient
            AND (j.args->>'invited_at')::timestamptz = i.invited_at
            AND (j.args->>'expires_at')::timestamptz = d.expires_at;
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE organization_invites DROP COLUMN email_delivery_status;
