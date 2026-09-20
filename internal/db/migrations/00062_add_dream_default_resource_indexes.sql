-- +goose Up

-- Exactly one reusable Dream-owned environment and system Agent may exist in a
-- workspace. Partial so a genuinely deleted row can be replaced.
create unique index environments_dream_default_environment_v1_key
    on environments (workspace_uuid)
    where deleted_at is null
      and metadata ->> 'internal_kind' = 'dream_default_environment';

create unique index agents_dream_default_agent_v1_key
    on agents (workspace_uuid)
    where deleted_at is null
      and metadata ->> 'internal_kind' = 'dream_default_agent';

-- +goose Down

drop index if exists agents_dream_default_agent_v1_key;
drop index if exists environments_dream_default_environment_v1_key;
