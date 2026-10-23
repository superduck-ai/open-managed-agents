-- +goose Up
ALTER TABLE llm_providers ADD COLUMN models jsonb NOT NULL DEFAULT '[]'::jsonb;

UPDATE llm_providers AS provider
SET models = (
    SELECT COALESCE(jsonb_agg(jsonb_strip_nulls(jsonb_build_object(
        'id', entry.model_id,
        'context_window', provider.model_context_windows -> entry.model_id,
        'max_output_tokens', provider.model_options -> entry.model_id -> 'max_output_tokens',
        'input_modalities', provider.model_options -> entry.model_id -> 'input_modalities'
    )) ORDER BY entry.position), '[]'::jsonb)
    FROM jsonb_array_elements_text(provider.model_ids) WITH ORDINALITY AS entry(model_id, position)
);

ALTER TABLE llm_providers DROP COLUMN model_ids, DROP COLUMN model_context_windows, DROP COLUMN model_options;

-- +goose Down
ALTER TABLE llm_providers
    ADD COLUMN model_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN model_context_windows jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN model_options jsonb NOT NULL DEFAULT '{}'::jsonb;

UPDATE llm_providers AS provider
SET model_ids = (
    SELECT COALESCE(jsonb_agg(entry.model -> 'id' ORDER BY entry.position), '[]'::jsonb)
    FROM jsonb_array_elements(provider.models) WITH ORDINALITY AS entry(model, position)
), model_context_windows = (
    SELECT COALESCE(jsonb_object_agg(model ->> 'id', model -> 'context_window')
        FILTER (WHERE model ? 'context_window'), '{}'::jsonb)
    FROM jsonb_array_elements(provider.models) AS model
), model_options = (
    SELECT COALESCE(jsonb_object_agg(model ->> 'id', model - 'id' - 'context_window')
        FILTER (WHERE model - 'id' - 'context_window' <> '{}'::jsonb), '{}'::jsonb)
    FROM jsonb_array_elements(provider.models) AS model
);

ALTER TABLE llm_providers DROP COLUMN models;
