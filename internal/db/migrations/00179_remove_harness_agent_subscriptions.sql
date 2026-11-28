-- +goose Up
ALTER TABLE harness_thread_state DROP COLUMN active_subscriptions;
ALTER TABLE harness_thread_hierarchy DROP COLUMN is_subscription_event;

-- +goose Down
ALTER TABLE harness_thread_hierarchy ADD COLUMN is_subscription_event boolean NOT NULL DEFAULT false;
ALTER TABLE harness_thread_state ADD COLUMN active_subscriptions json NOT NULL DEFAULT '{}';
