-- +goose Up
ALTER TABLE builtin_skills
    ADD COLUMN publisher text NOT NULL DEFAULT '',
    ADD COLUMN source_url text NOT NULL DEFAULT '',
    ADD COLUMN license text NOT NULL DEFAULT '',
    ADD COLUMN listed boolean NOT NULL DEFAULT true,
    ADD COLUMN revision bigint NOT NULL DEFAULT 1;
UPDATE builtin_skills SET listed = false WHERE latest_version IS NULL;
ALTER TABLE builtin_skill_versions
    ADD COLUMN skill_uuid uuid,
    ADD COLUMN state text NOT NULL DEFAULT 'published' CHECK (state IN ('draft', 'published')),
    ADD COLUMN notes text NOT NULL DEFAULT '';
UPDATE builtin_skill_versions v SET skill_uuid = s.uuid FROM builtin_skills s WHERE v.skill_id = s.id;
ALTER TABLE builtin_skill_versions ALTER COLUMN skill_uuid SET NOT NULL;
ALTER TABLE builtin_skill_versions ALTER COLUMN skill_id DROP NOT NULL;
CREATE UNIQUE INDEX builtin_skill_versions_uuid_version_key ON builtin_skill_versions(skill_uuid, version);
CREATE TABLE system_skill_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    skill_uuid uuid NOT NULL,
    actor_uuid uuid NOT NULL,
    action text NOT NULL,
    version text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX system_skill_events_skill_created_idx ON system_skill_events(skill_uuid, created_at DESC, uuid DESC);

-- +goose Down
DROP TABLE system_skill_events;
UPDATE builtin_skill_versions v SET skill_id = s.id FROM builtin_skills s WHERE v.skill_uuid = s.uuid;
ALTER TABLE builtin_skill_versions ALTER COLUMN skill_id SET NOT NULL;
DROP INDEX builtin_skill_versions_uuid_version_key;
ALTER TABLE builtin_skill_versions DROP COLUMN skill_uuid, DROP COLUMN state, DROP COLUMN notes;
ALTER TABLE builtin_skills DROP COLUMN publisher, DROP COLUMN source_url, DROP COLUMN license, DROP COLUMN listed, DROP COLUMN revision;
