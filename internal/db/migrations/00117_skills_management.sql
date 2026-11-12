-- +goose Up
ALTER TABLE skills ADD COLUMN icon_key text NOT NULL DEFAULT 'blocks';
ALTER TABLE builtin_skills
    DROP COLUMN IF EXISTS revision,
    ADD COLUMN icon_key text NOT NULL DEFAULT 'blocks',
    ADD COLUMN bundled boolean NOT NULL DEFAULT false,
    ADD COLUMN bundled_version text;
DROP TABLE IF EXISTS system_skill_events;

-- +goose Down
CREATE TABLE IF NOT EXISTS system_skill_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    skill_uuid uuid NOT NULL,
    actor_uuid uuid NOT NULL,
    action text NOT NULL,
    version text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE builtin_skills
    DROP COLUMN bundled_version,
    DROP COLUMN bundled,
    DROP COLUMN icon_key,
    ADD COLUMN revision bigint NOT NULL DEFAULT 1;
ALTER TABLE skills DROP COLUMN icon_key;
