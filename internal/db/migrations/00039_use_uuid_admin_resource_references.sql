-- +goose Up

alter table users
	add column organization_uuid uuid;

alter table organization_invites
	add column organization_uuid uuid;

alter table api_keys
	add column workspace_uuid uuid,
	add column created_by_user_uuid uuid;

alter table workspace_members
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column user_uuid uuid;

update users u
set organization_uuid = o.uuid
from organizations o
where o.id = u.organization_id;

update organization_invites i
set organization_uuid = o.uuid
from organizations o
where o.id = i.organization_id;

update api_keys ak
set workspace_uuid = w.uuid
from workspaces w
where w.id = ak.workspace_id;

update api_keys ak
set created_by_user_uuid = u.uuid
from users u
where u.id = ak.created_by_user_id;

update workspace_members wm
set organization_uuid = o.uuid
from organizations o
where o.id = wm.organization_id;

update workspace_members wm
set workspace_uuid = w.uuid
from workspaces w
where w.id = wm.workspace_id;

update workspace_members wm
set user_uuid = u.uuid
from users u
where u.id = wm.user_id;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from users
		where organization_uuid is null
	) then
		raise exception 'cannot migrate users organization references to UUID';
	end if;

	if exists (
		select 1
		from organization_invites
		where organization_uuid is null
	) then
		raise exception 'cannot migrate organization invite references to UUID';
	end if;

	if exists (
		select 1
		from api_keys
		where workspace_uuid is null
			or (created_by_user_id is not null and created_by_user_uuid is null)
	) then
		raise exception 'cannot migrate API key references to UUID';
	end if;

	if exists (
		select 1
		from workspace_members
		where organization_uuid is null
			or workspace_uuid is null
			or user_uuid is null
	) or exists (
		select 1
		from workspace_members wm
		join workspaces w on w.uuid = wm.workspace_uuid
		join users u on u.uuid = wm.user_uuid
		where w.organization_uuid <> wm.organization_uuid
			or u.organization_uuid <> wm.organization_uuid
	) then
		raise exception 'cannot migrate workspace member references to UUID';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists users_organization_email_active_v1_key;
drop index if exists users_organization_created_v1_idx;
drop index if exists organization_invites_org_created_v1_idx;
drop index if exists workspace_members_workspace_user_active_v1_key;
drop index if exists workspace_members_workspace_created_v1_idx;

alter table users
	alter column organization_uuid set not null,
	drop column organization_id;

alter table organization_invites
	alter column organization_uuid set not null,
	drop column organization_id;

alter table api_keys
	alter column workspace_uuid set not null,
	drop column workspace_id,
	drop column created_by_user_id;

alter table workspace_members
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column user_uuid set not null,
	drop column organization_id,
	drop column workspace_id,
	drop column user_id;

create unique index users_organization_email_active_v1_key
	on users (organization_uuid, lower(email))
	where deleted_at is null;

create index users_organization_created_v1_idx
	on users (organization_uuid, added_at desc, uuid desc)
	where deleted_at is null;

create index organization_invites_org_created_v1_idx
	on organization_invites (organization_uuid, invited_at desc, uuid desc);

create unique index workspace_members_workspace_user_active_v1_key
	on workspace_members (workspace_uuid, user_uuid)
	where deleted_at is null;

create index workspace_members_workspace_created_v1_idx
	on workspace_members (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;

-- +goose Down

alter table users
	add column organization_id bigint;

alter table organization_invites
	add column organization_id bigint;

alter table api_keys
	add column workspace_id bigint,
	add column created_by_user_id bigint;

alter table workspace_members
	add column organization_id bigint,
	add column workspace_id bigint,
	add column user_id bigint;

update users u
set organization_id = o.id
from organizations o
where o.uuid = u.organization_uuid;

update organization_invites i
set organization_id = o.id
from organizations o
where o.uuid = i.organization_uuid;

update api_keys ak
set workspace_id = w.id
from workspaces w
where w.uuid = ak.workspace_uuid;

update api_keys ak
set created_by_user_id = u.id
from users u
where u.uuid = ak.created_by_user_uuid;

update workspace_members wm
set organization_id = o.id
from organizations o
where o.uuid = wm.organization_uuid;

update workspace_members wm
set workspace_id = w.id
from workspaces w
where w.uuid = wm.workspace_uuid;

update workspace_members wm
set user_id = u.id
from users u
where u.uuid = wm.user_uuid;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from users
		where organization_id is null
	) or exists (
		select 1
		from organization_invites
		where organization_id is null
	) or exists (
		select 1
		from api_keys
		where workspace_id is null
			or (created_by_user_uuid is not null and created_by_user_id is null)
	) or exists (
		select 1
		from workspace_members
		where organization_id is null
			or workspace_id is null
			or user_id is null
	) then
		raise exception 'cannot restore internal resource references';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists users_organization_email_active_v1_key;
drop index if exists users_organization_created_v1_idx;
drop index if exists organization_invites_org_created_v1_idx;
drop index if exists workspace_members_workspace_user_active_v1_key;
drop index if exists workspace_members_workspace_created_v1_idx;

alter table users
	alter column organization_id set not null,
	drop column organization_uuid;

alter table organization_invites
	alter column organization_id set not null,
	drop column organization_uuid;

alter table api_keys
	alter column workspace_id set not null,
	drop column workspace_uuid,
	drop column created_by_user_uuid;

alter table workspace_members
	alter column organization_id set not null,
	alter column workspace_id set not null,
	alter column user_id set not null,
	drop column organization_uuid,
	drop column workspace_uuid,
	drop column user_uuid;

create unique index users_organization_email_active_v1_key
	on users (organization_id, lower(email))
	where deleted_at is null;

create index users_organization_created_v1_idx
	on users (organization_id, added_at desc, id desc)
	where deleted_at is null;

create index organization_invites_org_created_v1_idx
	on organization_invites (organization_id, invited_at desc, id desc);

create unique index workspace_members_workspace_user_active_v1_key
	on workspace_members (workspace_id, user_id)
	where deleted_at is null;

create index workspace_members_workspace_created_v1_idx
	on workspace_members (workspace_id, created_at desc, id desc)
	where deleted_at is null;
