-- +goose Up
ALTER TABLE builtin_skills ADD COLUMN listed boolean NOT NULL DEFAULT true, ADD COLUMN revision bigint NOT NULL DEFAULT 1;
UPDATE builtin_skills SET listed = false WHERE latest_version IS NULL;
CREATE TABLE builtin_skill_versions_next (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    external_id text not null,
    skill_uuid uuid not null,
    skill_external_id text not null,
    version text not null,
    state text not null default 'published' check (state in ('draft', 'published')),
    name text not null,
    description text not null default '',
    directory text not null,
    s3_bucket text not null,
    s3_key text not null,
    size_bytes bigint not null check (size_bytes >= 0),
    sha256 text not null,
    created_at timestamptz not null default now(),
    deleted_at timestamptz
);
INSERT INTO builtin_skill_versions_next (id, uuid, external_id, skill_uuid, skill_external_id, version, state, name, description, directory, s3_bucket, s3_key, size_bytes, sha256, created_at, deleted_at) OVERRIDING SYSTEM VALUE
SELECT v.id, v.uuid, v.external_id, s.uuid, v.skill_external_id, v.version, 'published', v.name, v.description, v.directory, v.s3_bucket, v.s3_key, v.size_bytes, v.sha256, v.created_at, v.deleted_at
FROM builtin_skill_versions v LEFT JOIN builtin_skills s ON s.id = v.skill_id;
DROP TABLE builtin_skill_versions;
ALTER TABLE builtin_skill_versions_next RENAME TO builtin_skill_versions;
ALTER TABLE builtin_skill_versions ADD PRIMARY KEY (id), ADD UNIQUE (uuid), ADD UNIQUE (external_id), ADD UNIQUE (skill_uuid, version);
SELECT setval(pg_get_serial_sequence('builtin_skill_versions', 'id'), COALESCE(MAX(id), 1), COUNT(*) > 0) FROM builtin_skill_versions;
CREATE INDEX builtin_skill_versions_skill_created_v1_idx ON builtin_skill_versions (skill_uuid, created_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX builtin_skill_versions_skill_version_v1_idx ON builtin_skill_versions (skill_external_id, version) WHERE deleted_at IS NULL;
CREATE TABLE system_skill_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    skill_uuid uuid NOT NULL,
    actor_uuid uuid NOT NULL,
    action text NOT NULL,
    version text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX system_skill_events_skill_created_idx ON system_skill_events(skill_uuid, created_at DESC, id DESC);

-- +goose Down
DROP TABLE system_skill_events;
CREATE TABLE builtin_skill_versions_next (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    external_id text not null,
    skill_id bigint not null,
    skill_external_id text not null,
    version text not null,
    name text not null,
    description text not null default '',
    directory text not null,
    s3_bucket text not null,
    s3_key text not null,
    size_bytes bigint not null check (size_bytes >= 0),
    sha256 text not null,
    created_at timestamptz not null default now(),
    deleted_at timestamptz
);
INSERT INTO builtin_skill_versions_next (id, uuid, external_id, skill_id, skill_external_id, version, name, description, directory, s3_bucket, s3_key, size_bytes, sha256, created_at, deleted_at) OVERRIDING SYSTEM VALUE
SELECT v.id, v.uuid, v.external_id, s.id, v.skill_external_id, v.version, v.name, v.description, v.directory, v.s3_bucket, v.s3_key, v.size_bytes, v.sha256, v.created_at, v.deleted_at
FROM builtin_skill_versions v LEFT JOIN builtin_skills s ON s.uuid = v.skill_uuid;
DROP TABLE builtin_skill_versions;
ALTER TABLE builtin_skill_versions_next RENAME TO builtin_skill_versions;
ALTER TABLE builtin_skill_versions ADD PRIMARY KEY (id), ADD UNIQUE (uuid), ADD UNIQUE (external_id), ADD UNIQUE (skill_id, version);
SELECT setval(pg_get_serial_sequence('builtin_skill_versions', 'id'), COALESCE(MAX(id), 1), COUNT(*) > 0) FROM builtin_skill_versions;
CREATE INDEX builtin_skill_versions_skill_created_v1_idx ON builtin_skill_versions (skill_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;
CREATE INDEX builtin_skill_versions_skill_version_v1_idx ON builtin_skill_versions (skill_external_id, version) WHERE deleted_at IS NULL;
ALTER TABLE builtin_skills DROP COLUMN listed, DROP COLUMN revision;
