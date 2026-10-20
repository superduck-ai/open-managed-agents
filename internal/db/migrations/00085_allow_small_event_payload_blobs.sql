-- +goose Up
ALTER TABLE event_payload_blobs DROP CONSTRAINT event_payload_blobs_size_bytes_check;
ALTER TABLE event_payload_blobs ADD CONSTRAINT event_payload_blobs_size_bytes_check CHECK (size_bytes > 0);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM event_payload_blobs WHERE size_bytes <= 32768) THEN
        RAISE EXCEPTION 'restore small externalized event payloads before rollback';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE event_payload_blobs DROP CONSTRAINT event_payload_blobs_size_bytes_check;
ALTER TABLE event_payload_blobs ADD CONSTRAINT event_payload_blobs_size_bytes_check CHECK (size_bytes > 32768);
