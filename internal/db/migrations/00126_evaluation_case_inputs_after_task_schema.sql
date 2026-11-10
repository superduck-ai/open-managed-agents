-- +goose Up
ALTER TABLE evaluation_cases
    ADD COLUMN IF NOT EXISTS inputs jsonb NOT NULL DEFAULT '[]'::jsonb;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'evaluation_cases_has_input'
    ) THEN
        ALTER TABLE evaluation_cases
            ADD CONSTRAINT evaluation_cases_has_input CHECK (
                jsonb_typeof(inputs) = 'array'
                AND (char_length(query) > 0 OR jsonb_array_length(inputs) > 0)
            );
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE evaluation_cases DROP CONSTRAINT IF EXISTS evaluation_cases_has_input;
ALTER TABLE evaluation_cases DROP COLUMN IF EXISTS inputs;
