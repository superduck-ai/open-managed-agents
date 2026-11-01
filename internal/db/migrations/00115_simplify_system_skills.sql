-- +goose Up
ALTER TABLE builtin_skills DROP COLUMN publisher, DROP COLUMN source_url, DROP COLUMN license;
ALTER TABLE builtin_skill_versions DROP COLUMN notes, DROP COLUMN skill_id;

-- +goose Down
ALTER TABLE builtin_skills
    ADD COLUMN publisher text NOT NULL DEFAULT '',
    ADD COLUMN source_url text NOT NULL DEFAULT '',
    ADD COLUMN license text NOT NULL DEFAULT '';
ALTER TABLE builtin_skill_versions
    ADD COLUMN notes text NOT NULL DEFAULT '',
    ADD COLUMN skill_id bigint;
UPDATE builtin_skill_versions v SET skill_id = s.id FROM builtin_skills s WHERE v.skill_uuid = s.uuid;
ALTER TABLE builtin_skill_versions ADD CONSTRAINT builtin_skill_versions_skill_version_key UNIQUE (skill_id, version);
CREATE INDEX builtin_skill_versions_skill_created_v1_idx
    ON builtin_skill_versions (skill_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;
