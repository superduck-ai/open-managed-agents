-- +goose Up

alter table organizations
	drop column external_id;

-- +goose Down

alter table organizations
	add column external_id text;

update organizations
set external_id = 'org_' || replace(CAST(uuid AS text), '-', '');

alter table organizations
	alter column external_id set not null,
	add constraint organizations_external_id_key unique (external_id);
