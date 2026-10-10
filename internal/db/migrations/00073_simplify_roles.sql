-- +goose Up
UPDATE users SET role = 'user' WHERE role IN ('developer', 'billing', 'claude_code_user');
UPDATE organization_invites SET role = 'user' WHERE role IN ('developer', 'billing', 'claude_code_user');
UPDATE workspace_members SET workspace_role = 'workspace_user'
WHERE workspace_role IN ('workspace_developer', 'workspace_restricted_developer', 'workspace_billing');

ALTER TABLE users DROP CONSTRAINT users_role_check,
    ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'admin'));
ALTER TABLE organization_invites DROP CONSTRAINT organization_invites_role_check,
    ADD CONSTRAINT organization_invites_role_check CHECK (role IN ('user', 'admin'));
ALTER TABLE workspace_members DROP CONSTRAINT workspace_members_role_check,
    ADD CONSTRAINT workspace_members_role_check CHECK (workspace_role IN ('workspace_user', 'workspace_admin'));

-- +goose Down
ALTER TABLE users DROP CONSTRAINT users_role_check,
    ADD CONSTRAINT users_role_check CHECK (role IN ('user', 'developer', 'billing', 'admin', 'claude_code_user'));
ALTER TABLE organization_invites DROP CONSTRAINT organization_invites_role_check,
    ADD CONSTRAINT organization_invites_role_check CHECK (role IN ('user', 'developer', 'billing', 'admin', 'claude_code_user'));
ALTER TABLE workspace_members DROP CONSTRAINT workspace_members_role_check,
    ADD CONSTRAINT workspace_members_role_check CHECK (workspace_role IN ('workspace_user', 'workspace_developer', 'workspace_restricted_developer', 'workspace_admin', 'workspace_billing'));
