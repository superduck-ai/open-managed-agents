-- +goose NO TRANSACTION

-- +goose Up
with ranked as (
	select
		id,
		display_title,
		'skill_' || id::text as duplicate_suffix,
		row_number() over (
			partition by workspace_id, display_title
			order by updated_at desc, id desc
		) as duplicate_rank
	from skills
	where deleted_at is null
		and display_title is not null
),
renamed as (
	select
		id,
		display_title || ' (' || duplicate_suffix || ')' as display_title
	from ranked
	where duplicate_rank > 1
)
update skills
set display_title = renamed.display_title
from renamed
where skills.id = renamed.id;

create unique index concurrently if not exists skills_workspace_display_title_active_key
	on skills (workspace_id, display_title)
	where deleted_at is null and display_title is not null;

-- +goose Down
-- Duplicate-title renames are intentionally not reversed; restoring them would
-- recreate active duplicates and violate the API contract this migration adds.
drop index concurrently if exists skills_workspace_display_title_active_key;
