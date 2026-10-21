-- +goose Up
CREATE TABLE brain_workers (
	id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
	external_id text NOT NULL UNIQUE,
	max_sessions integer NOT NULL,
	busy_sessions integer NOT NULL,
	resident_sessions integer NOT NULL,
	admission_failures integer NOT NULL DEFAULT 0,
	circuit_failures integer NOT NULL DEFAULT 0,
	circuit_open_until timestamptz,
	last_seen_at timestamptz NOT NULL DEFAULT NOW(),
	created_at timestamptz NOT NULL DEFAULT NOW(),
	updated_at timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX brain_workers_last_seen_idx ON brain_workers (last_seen_at);

ALTER TABLE code_sessions ADD COLUMN IF NOT EXISTS worker_claim_acked_at timestamptz;

-- +goose Down
ALTER TABLE code_sessions DROP COLUMN IF EXISTS worker_claim_acked_at;

DROP TABLE IF EXISTS brain_workers;
