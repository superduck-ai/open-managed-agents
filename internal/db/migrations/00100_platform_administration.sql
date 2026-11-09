-- +goose Up
CREATE TABLE platform_admins (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    account_uuid uuid NOT NULL UNIQUE,
    granted_at timestamptz NOT NULL DEFAULT now(),
    granted_by text NOT NULL,
    revoked_at timestamptz,
    revoked_by text
);

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

ALTER TABLE organizations ADD COLUMN management_version bigint NOT NULL DEFAULT 1;
ALTER TABLE organization_invites ADD COLUMN platform_actor_uuid uuid;

CREATE TABLE platform_admin_invite_deliveries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    invitation_uuid uuid NOT NULL,
    recipient text NOT NULL,
    expires_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'cancelled')),
    attempts integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION bump_organization_management_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.name IS DISTINCT FROM OLD.name OR NEW.profile IS DISTINCT FROM OLD.profile THEN
        NEW.management_version := OLD.management_version + 1;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER organization_management_version BEFORE UPDATE ON organizations
    FOR EACH ROW EXECUTE FUNCTION bump_organization_management_version();

-- +goose StatementBegin
CREATE FUNCTION bump_member_organization_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE organizations SET management_version = management_version + 1 WHERE uuid = NEW.organization_uuid;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER member_organization_version AFTER INSERT OR UPDATE OF role, deleted_at ON users
    FOR EACH ROW EXECUTE FUNCTION bump_member_organization_version();
CREATE TRIGGER invite_organization_version AFTER INSERT OR UPDATE OF status, expires_at, deleted_at ON organization_invites
    FOR EACH ROW EXECUTE FUNCTION bump_member_organization_version();

-- +goose Down
DROP TRIGGER invite_organization_version ON organization_invites;
DROP TRIGGER member_organization_version ON users;
DROP FUNCTION bump_member_organization_version();
DROP TRIGGER organization_management_version ON organizations;
DROP FUNCTION bump_organization_management_version();
ALTER TABLE organization_invites DROP COLUMN platform_actor_uuid;
ALTER TABLE organizations DROP COLUMN management_version;
DROP TABLE platform_admin_invite_deliveries;
DROP TABLE platform_admin_create_requests;
DROP TABLE platform_admins;
