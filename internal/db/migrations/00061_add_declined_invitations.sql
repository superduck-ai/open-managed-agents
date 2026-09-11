-- +goose Up
ALTER TABLE organization_invites DROP CONSTRAINT organization_invites_status_check;
ALTER TABLE organization_invites ADD CONSTRAINT organization_invites_status_check
    CHECK (status IN ('accepted', 'expired', 'deleted', 'pending', 'declined'));

-- +goose Down
UPDATE organization_invites SET status = 'deleted', deleted_at = COALESCE(deleted_at, NOW()) WHERE status = 'declined';
ALTER TABLE organization_invites DROP CONSTRAINT organization_invites_status_check;
ALTER TABLE organization_invites ADD CONSTRAINT organization_invites_status_check
    CHECK (status IN ('accepted', 'expired', 'deleted', 'pending'));
