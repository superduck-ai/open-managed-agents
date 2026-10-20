-- +goose Up
CREATE TABLE llm_model_prices (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 external_id text NOT NULL DEFAULT gen_random_uuid()::text,
 organization_uuid uuid NOT NULL,
 workspace_uuid uuid NOT NULL,
 provider_uuid uuid NOT NULL,
 model text NOT NULL,
 input numeric(24,12) CHECK(input >= 0),
 output numeric(24,12) CHECK(output >= 0),
 cache_read numeric(24,12) CHECK(cache_read >= 0),
 cache_write numeric(24,12) CHECK(cache_write >= 0),
 cache_write_5m numeric(24,12) CHECK(cache_write_5m >= 0),
 cache_write_1h numeric(24,12) CHECK(cache_write_1h >= 0),
 provenance jsonb NOT NULL DEFAULT '{}',
 version text NOT NULL DEFAULT gen_random_uuid()::text,
 updated_by text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(organization_uuid,workspace_uuid,provider_uuid,model)
);
-- +goose Down
DROP TABLE llm_model_prices;
