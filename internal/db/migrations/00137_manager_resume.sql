-- +goose Up
ALTER TABLE environment_sandboxes ADD COLUMN manager_resume_phase text NOT NULL DEFAULT ''
    CHECK (manager_resume_phase IN ('', 'checking', 'draining', 'starting')),
    ADD COLUMN manager_version text NOT NULL DEFAULT '';


-- +goose Down
ALTER TABLE environment_sandboxes DROP COLUMN manager_resume_phase, DROP COLUMN manager_version;
