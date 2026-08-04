-- +goose Up

alter table console_api_keys
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column api_key_ref_uuid uuid,
	add column created_by_user_ref_uuid uuid;

update console_api_keys c
set organization_uuid = CAST(c.org_uuid AS uuid),
	api_key_ref_uuid = (
		select k.uuid
		from api_keys k
		where k.external_id = c.external_id
			or k.external_id = c.api_key_uuid
		limit 1
	),
	workspace_uuid = (
		select w.uuid
		from workspaces w
		where w.organization_uuid = CAST(c.org_uuid AS uuid)
			and (
				w.external_id = c.workspace_id
				or CAST(w.uuid AS text) = c.workspace_id
				or c.workspace_id = 'default'
			)
		order by
			case
				when w.external_id = c.workspace_id then 0
				when CAST(w.uuid AS text) = c.workspace_id then 1
				when w.external_id = 'workspace_default' then 2
				when lower(w.name) = 'default' then 3
				else 4
			end,
			w.created_at,
			w.uuid
		limit 1
	),
	created_by_user_ref_uuid = (
		select u.uuid
		from users u
		where u.organization_uuid = CAST(c.org_uuid AS uuid)
			and (
				CAST(u.uuid AS text) = c.created_by_user_uuid
				or u.external_id = c.created_by_user_uuid
				or 'user_' || left(replace(CAST(u.uuid AS text), '-', ''), 24) = c.created_by_user_uuid
			)
		limit 1
	);

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1 from console_api_keys
		where organization_uuid is null or workspace_uuid is null or api_key_ref_uuid is null
	) then
		raise exception 'console_api_keys contains an unmapped core UUID reference';
	end if;
	if exists (
		select 1 from console_api_keys
		where created_by_user_uuid is not null
			and created_by_user_uuid <> ''
			and created_by_user_ref_uuid is null
	) then
		raise exception 'console_api_keys contains an unmapped created_by_user_uuid';
	end if;
end $$;
-- +goose StatementEnd

alter table console_api_keys
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column api_key_ref_uuid set not null,
	drop column org_uuid,
	drop column api_key_uuid,
	drop column created_by_user_uuid;

drop index console_api_keys_workspace_created_idx;

create unique index console_api_keys_api_key_ref_uuid_key
	on console_api_keys (api_key_ref_uuid);
create index console_api_keys_org_archived_v2_idx
	on console_api_keys (organization_uuid, archived_at);
create index console_api_keys_org_workspace_archived_v2_idx
	on console_api_keys (organization_uuid, workspace_uuid, archived_at);
create index console_api_keys_workspace_created_v2_idx
	on console_api_keys (workspace_uuid, created_at desc, uuid desc);

alter table workbench_prompts
	add column uuid uuid not null default gen_random_uuid(),
	add column organization_uuid uuid,
	add column workspace_uuid uuid;
alter table workbench_prompt_revisions
	add column uuid uuid not null default gen_random_uuid(),
	add column organization_uuid uuid,
	add column prompt_ref_uuid uuid;
alter table workbench_prompt_kv
	add column uuid uuid not null default gen_random_uuid(),
	add column organization_uuid uuid,
	add column prompt_ref_uuid uuid;
alter table workbench_evaluations
	add column uuid uuid not null default gen_random_uuid(),
	add column organization_uuid uuid,
	add column revision_ref_uuid uuid;
alter table workbench_generated_test_cases
	add column uuid uuid not null default gen_random_uuid(),
	add column organization_uuid uuid;

update workbench_prompts p
set organization_uuid = CAST(p.org_uuid AS uuid),
	workspace_uuid = (
		select w.uuid
		from workspaces w
		where w.organization_uuid = CAST(p.org_uuid AS uuid)
			and (
				w.external_id = p.workspace_id
				or CAST(w.uuid AS text) = p.workspace_id
				or p.workspace_id = 'default'
			)
		order by
			case
				when w.external_id = p.workspace_id then 0
				when CAST(w.uuid AS text) = p.workspace_id then 1
				when w.external_id = 'workspace_default' then 2
				when lower(w.name) = 'default' then 3
				else 4
			end,
			w.created_at,
			w.uuid
		limit 1
	);

update workbench_prompt_revisions r
set organization_uuid = CAST(r.org_uuid AS uuid),
	prompt_ref_uuid = (
		select p.uuid
		from workbench_prompts p
		where p.organization_uuid = CAST(r.org_uuid AS uuid)
			and p.prompt_uuid = r.prompt_uuid
	);

update workbench_prompt_kv k
set organization_uuid = CAST(k.org_uuid AS uuid),
	prompt_ref_uuid = (
		select p.uuid
		from workbench_prompts p
		where p.organization_uuid = CAST(k.org_uuid AS uuid)
			and p.prompt_uuid = k.prompt_uuid
	);

update workbench_evaluations e
set organization_uuid = CAST(e.org_uuid AS uuid),
	revision_ref_uuid = (
		select r.uuid
		from workbench_prompt_revisions r
		where r.organization_uuid = CAST(e.org_uuid AS uuid)
			and r.revision_uuid = e.revision_uuid
		order by r.created_at desc, r.uuid desc
		limit 1
	);

update workbench_generated_test_cases t
set organization_uuid = CAST(t.org_uuid AS uuid);

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1 from workbench_prompts
		where organization_uuid is null or workspace_uuid is null
	) then
		raise exception 'workbench_prompts contains an unmapped tenant UUID reference';
	end if;
	if exists (
		select 1 from workbench_prompt_revisions
		where organization_uuid is null or prompt_ref_uuid is null
	) then
		raise exception 'workbench_prompt_revisions contains an unmapped prompt reference';
	end if;
	if exists (
		select 1 from workbench_prompt_kv
		where organization_uuid is null or prompt_ref_uuid is null
	) then
		raise exception 'workbench_prompt_kv contains an unmapped prompt reference';
	end if;
	if exists (
		select 1 from workbench_evaluations
		where organization_uuid is null or revision_ref_uuid is null
	) then
		raise exception 'workbench_evaluations contains an unmapped revision reference';
	end if;
end $$;
-- +goose StatementEnd

alter table workbench_prompts
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	drop column org_uuid;
alter table workbench_prompt_revisions
	alter column organization_uuid set not null,
	alter column prompt_ref_uuid set not null,
	drop column org_uuid;
alter table workbench_prompt_kv
	alter column organization_uuid set not null,
	alter column prompt_ref_uuid set not null,
	drop column org_uuid;
alter table workbench_evaluations
	alter column organization_uuid set not null,
	alter column revision_ref_uuid set not null,
	drop column org_uuid;
alter table workbench_generated_test_cases
	alter column organization_uuid set not null,
	drop column org_uuid;

create unique index workbench_prompts_uuid_key on workbench_prompts (uuid);
create unique index workbench_prompt_revisions_uuid_key on workbench_prompt_revisions (uuid);
create unique index workbench_prompt_kv_uuid_key on workbench_prompt_kv (uuid);
create unique index workbench_evaluations_uuid_key on workbench_evaluations (uuid);
create unique index workbench_generated_test_cases_uuid_key on workbench_generated_test_cases (uuid);
create unique index workbench_prompts_org_prompt_key
	on workbench_prompts (organization_uuid, prompt_uuid);
create index idx_workbench_prompts_org_deleted
	on workbench_prompts (organization_uuid, deleted_at);
create unique index workbench_prompt_revisions_org_prompt_revision_key
	on workbench_prompt_revisions (organization_uuid, prompt_ref_uuid, revision_uuid);
create unique index workbench_prompt_kv_org_prompt_key_key
	on workbench_prompt_kv (organization_uuid, prompt_ref_uuid, key);
create unique index workbench_evaluations_org_evaluation_key
	on workbench_evaluations (organization_uuid, evaluation_uuid);
create index workbench_prompts_workspace_updated_v2_idx
	on workbench_prompts (workspace_uuid, updated_at desc, uuid desc)
	where deleted_at is null;
create index workbench_prompt_revisions_prompt_created_v2_idx
	on workbench_prompt_revisions (prompt_ref_uuid, created_at desc, uuid desc);
create index workbench_evaluations_revision_created_v2_idx
	on workbench_evaluations (revision_ref_uuid, created_at asc, uuid asc);
create index workbench_generated_test_cases_org_created_v2_idx
	on workbench_generated_test_cases (organization_uuid, created_at asc, uuid asc);

-- +goose Down

alter table console_api_keys
	add column org_uuid text,
	add column api_key_uuid text,
	add column created_by_user_uuid text;

update console_api_keys c
set org_uuid = CAST(c.organization_uuid AS text),
	api_key_uuid = (
		select k.external_id from api_keys k where k.uuid = c.api_key_ref_uuid
	),
	created_by_user_uuid = CAST(c.created_by_user_ref_uuid AS text);

alter table console_api_keys
	alter column org_uuid set not null,
	alter column api_key_uuid set not null,
	drop column organization_uuid,
	drop column workspace_uuid,
	drop column api_key_ref_uuid,
	drop column created_by_user_ref_uuid;

create unique index console_api_keys_api_key_uuid_key on console_api_keys (api_key_uuid);
create index console_api_keys_org_archived_idx on console_api_keys (org_uuid, archived_at);
create index console_api_keys_org_workspace_archived_idx on console_api_keys (org_uuid, workspace_id, archived_at);
create index console_api_keys_workspace_created_idx on console_api_keys (workspace_id, created_at);

alter table workbench_prompts add column org_uuid text;
alter table workbench_prompt_revisions add column org_uuid text;
alter table workbench_prompt_kv add column org_uuid text;
alter table workbench_evaluations add column org_uuid text;
alter table workbench_generated_test_cases add column org_uuid text;

update workbench_prompts set org_uuid = CAST(organization_uuid AS text);
update workbench_prompt_revisions set org_uuid = CAST(organization_uuid AS text);
update workbench_prompt_kv set org_uuid = CAST(organization_uuid AS text);
update workbench_evaluations set org_uuid = CAST(organization_uuid AS text);
update workbench_generated_test_cases set org_uuid = CAST(organization_uuid AS text);

alter table workbench_prompts
	alter column org_uuid set not null,
	drop column uuid,
	drop column organization_uuid,
	drop column workspace_uuid;
alter table workbench_prompt_revisions
	alter column org_uuid set not null,
	drop column uuid,
	drop column organization_uuid,
	drop column prompt_ref_uuid;
alter table workbench_prompt_kv
	alter column org_uuid set not null,
	drop column uuid,
	drop column organization_uuid,
	drop column prompt_ref_uuid;
alter table workbench_evaluations
	alter column org_uuid set not null,
	drop column uuid,
	drop column organization_uuid,
	drop column revision_ref_uuid;
alter table workbench_generated_test_cases
	alter column org_uuid set not null,
	drop column uuid,
	drop column organization_uuid;

create unique index workbench_prompts_org_prompt_key
	on workbench_prompts (org_uuid, prompt_uuid);
create index idx_workbench_prompts_org_deleted
	on workbench_prompts (org_uuid, deleted_at);
create unique index workbench_prompt_revisions_org_prompt_revision_key
	on workbench_prompt_revisions (org_uuid, prompt_uuid, revision_uuid);
create index idx_workbench_prompt_revisions_org_prompt_created
	on workbench_prompt_revisions (org_uuid, prompt_uuid, created_at desc, id desc);
create unique index workbench_prompt_kv_org_prompt_key_key
	on workbench_prompt_kv (org_uuid, prompt_uuid, key);
create unique index workbench_evaluations_org_evaluation_key
	on workbench_evaluations (org_uuid, evaluation_uuid);
create index idx_workbench_evaluations_org_revision_created
	on workbench_evaluations (org_uuid, revision_uuid, created_at asc, id asc);
create index idx_workbench_generated_test_cases_org_id
	on workbench_generated_test_cases (org_uuid, id asc);
