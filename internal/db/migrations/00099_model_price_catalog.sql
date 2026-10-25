-- +goose Up
CREATE TABLE model_price_catalog (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 external_id text NOT NULL DEFAULT gen_random_uuid()::text,
 source text NOT NULL,
 model text NOT NULL,
 price jsonb NOT NULL,
 original jsonb NOT NULL,
 fetched_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(source,model)
);
-- +goose Down
DROP TABLE model_price_catalog;
