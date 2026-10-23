-- +goose Up
ALTER TABLE llm_providers ADD COLUMN models jsonb NOT NULL DEFAULT '[]'::jsonb;

UPDATE llm_providers AS provider
SET models = (
    SELECT COALESCE(jsonb_agg(jsonb_build_object('id', entry.model_id)
        ORDER BY entry.position), '[]'::jsonb)
    FROM jsonb_array_elements_text(provider.model_ids) WITH ORDINALITY AS entry(model_id, position)
);

ALTER TABLE llm_providers DROP COLUMN model_ids;

-- +goose Down
ALTER TABLE llm_providers ADD COLUMN model_ids jsonb NOT NULL DEFAULT '[]'::jsonb;

UPDATE llm_providers AS provider
SET model_ids = (
    SELECT COALESCE(jsonb_agg(entry.model -> 'id' ORDER BY entry.position), '[]'::jsonb)
    FROM jsonb_array_elements(provider.models) WITH ORDINALITY AS entry(model, position)
);

ALTER TABLE llm_providers DROP COLUMN models;
