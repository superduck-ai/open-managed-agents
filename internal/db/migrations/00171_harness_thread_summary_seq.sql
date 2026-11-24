-- +goose Up
ALTER TABLE harness_thread_hierarchy DROP COLUMN external_id;
ALTER TABLE harness_thread_hierarchy RENAME COLUMN uuid TO thread_uuid;
ALTER TABLE harness_thread_hierarchy RENAME COLUMN spawn_message_order TO spawn_message_seq;
ALTER TABLE harness_thread_hierarchy
    RENAME CONSTRAINT harness_thread_hierarchy_uuid_key TO harness_thread_hierarchy_thread_uuid_key;

ALTER TABLE harness_compaction_summaries
    DROP COLUMN uuid,
    DROP COLUMN external_id;
ALTER TABLE harness_compaction_summaries RENAME COLUMN up_to_order TO up_to_seq;
ALTER TABLE harness_compaction_summaries
    RENAME CONSTRAINT harness_compaction_summaries_up_to_order_check TO harness_compaction_summaries_up_to_seq_check;

-- +goose Down
ALTER TABLE harness_compaction_summaries
    RENAME CONSTRAINT harness_compaction_summaries_up_to_seq_check TO harness_compaction_summaries_up_to_order_check;
ALTER TABLE harness_compaction_summaries RENAME COLUMN up_to_seq TO up_to_order;
ALTER TABLE harness_compaction_summaries
    ADD COLUMN uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    ADD COLUMN external_id text NOT NULL DEFAULT ('hsum_' || gen_random_uuid());
ALTER TABLE harness_compaction_summaries ALTER COLUMN external_id DROP DEFAULT;

ALTER TABLE harness_thread_hierarchy
    RENAME CONSTRAINT harness_thread_hierarchy_thread_uuid_key TO harness_thread_hierarchy_uuid_key;
ALTER TABLE harness_thread_hierarchy RENAME COLUMN spawn_message_seq TO spawn_message_order;
ALTER TABLE harness_thread_hierarchy RENAME COLUMN thread_uuid TO uuid;
ALTER TABLE harness_thread_hierarchy ADD COLUMN external_id text;
UPDATE harness_thread_hierarchy SET external_id = 'hthr_' || uuid;
ALTER TABLE harness_thread_hierarchy ALTER COLUMN external_id SET NOT NULL;
