-- +goose Up
CREATE TABLE transcript_archives (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,                    -- "tarc_<uuid>"
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    code_session_external_id text NOT NULL,
    from_sequence_num bigint NOT NULL,
    to_sequence_num bigint NOT NULL,
    event_count int NOT NULL CHECK (event_count > 0),
    codec text NOT NULL CHECK (codec IN ('jsonl+zstd')),
    bucket text NOT NULL,
    object_key text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes > 0),
    raw_bytes  bigint NOT NULL CHECK (raw_bytes > 0),
    sha256 text NOT NULL,
    state text NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'attached', 'deleting')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (to_sequence_num >= from_sequence_num)
);

CREATE UNIQUE INDEX transcript_archives_range_v1_key
    ON transcript_archives (code_session_uuid, from_sequence_num)
    WHERE state <> 'deleting';

CREATE INDEX transcript_archives_session_v1_idx
    ON transcript_archives (workspace_uuid, code_session_external_id, from_sequence_num);

CREATE INDEX transcript_archives_cleanup_v1_idx
    ON transcript_archives (updated_at, uuid);

-- +goose Down
-- 归档段是已删除数据的唯一副本，存在 attached 段时拒绝回滚。
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM transcript_archives WHERE state = 'attached') THEN
        RAISE EXCEPTION 'restore archived transcript segments before rollback';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE transcript_archives;
