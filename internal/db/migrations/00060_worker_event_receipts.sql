-- +goose Up
CREATE TABLE code_session_worker_event_receipts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    worker_epoch bigint NOT NULL,
    event_external_id text NOT NULL,
    scope text NOT NULL,
    model_request_id text NOT NULL,
    block_index integer,
    payload_hash text NOT NULL,
    CONSTRAINT code_session_worker_event_receipt_identity UNIQUE (code_session_uuid, worker_epoch, event_external_id)
);

CREATE INDEX code_session_worker_event_receipt_blocks
    ON code_session_worker_event_receipts (code_session_uuid, worker_epoch, model_request_id, id DESC)
    WHERE block_index IS NOT NULL;

-- +goose Down
DROP TABLE code_session_worker_event_receipts;
