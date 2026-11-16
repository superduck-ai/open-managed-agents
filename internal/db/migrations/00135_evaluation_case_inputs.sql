-- +goose Up
ALTER TABLE evaluation_cases
    ADD COLUMN inputs jsonb NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE evaluation_cases
    ADD CONSTRAINT evaluation_cases_has_input CHECK (
        jsonb_typeof(inputs) = 'array'
        AND (char_length(query) > 0 OR jsonb_array_length(inputs) > 0)
    );

-- +goose Down
ALTER TABLE evaluation_cases DROP CONSTRAINT evaluation_cases_has_input;
ALTER TABLE evaluation_cases DROP COLUMN inputs;
