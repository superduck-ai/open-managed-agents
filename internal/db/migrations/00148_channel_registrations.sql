-- +goose Up
create table if not exists channel_registrations (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	kind text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	channel_external_id text not null default '',
	name text not null default '',
	phase text not null default 'waiting',
	url text not null default '',
	expires_at timestamptz not null default now(),
	next_poll_at timestamptz not null default now(),
	interval_seconds integer not null default 5,
	domain text not null default 'feishu',
	owner text not null default '',
	heartbeat_at timestamptz not null default now(),
	device_code_envelope jsonb,
	app_id text not null default '',
	app_secret_envelope jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint channel_registrations_id_pk primary key (id),
	constraint channel_registrations_uuid_key unique (uuid),
	constraint channel_registrations_external_id_key unique (external_id),
	constraint channel_registrations_kind_check check (kind in ('authorization', 'qr')),
	constraint channel_registrations_phase_check check (phase in ('waiting', 'bound', 'expired', 'denied', 'unknown')),
	constraint channel_registrations_domain_check check (domain in ('feishu', 'lark'))
);

create unique index if not exists channel_registrations_qr_channel_v1_idx
	on channel_registrations (channel_external_id)
	where kind = 'qr';

create index if not exists channel_registrations_heartbeat_v1_idx
	on channel_registrations (phase, heartbeat_at);

create index if not exists channel_registrations_workspace_v1_idx
	on channel_registrations (organization_uuid, workspace_uuid, created_at desc);

-- +goose Down
drop table if exists channel_registrations;
