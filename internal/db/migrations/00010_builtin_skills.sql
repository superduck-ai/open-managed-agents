-- +goose Up
create table if not exists builtin_skills (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	display_title text not null,
	latest_version text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint builtin_skills_id_pk primary key (id),
	constraint builtin_skills_uuid_key unique (uuid),
	constraint builtin_skills_external_id_key unique (external_id)
);

create index if not exists builtin_skills_created_v1_idx
	on builtin_skills (created_at desc, id desc)
	where deleted_at is null;

create table if not exists builtin_skill_versions (
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
	size_bytes bigint not null,
	sha256 text not null,
	created_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint builtin_skill_versions_id_pk primary key (id),
	constraint builtin_skill_versions_uuid_key unique (uuid),
	constraint builtin_skill_versions_external_id_key unique (external_id),
	constraint builtin_skill_versions_skill_version_key unique (skill_id, version),
	constraint builtin_skill_versions_size_bytes_non_negative check (size_bytes >= 0)
);

create index if not exists builtin_skill_versions_skill_created_v1_idx
	on builtin_skill_versions (skill_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists builtin_skill_versions_skill_version_v1_idx
	on builtin_skill_versions (skill_external_id, version)
	where deleted_at is null;

-- +goose Down
drop table if exists builtin_skill_versions;
drop table if exists builtin_skills;
