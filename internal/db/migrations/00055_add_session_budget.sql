-- +goose Up

-- Session-level budget (max_list_cost): hard cap in US cents on a single
-- session. NULL means no budget. Shape:
-- {"type":"limit","max_list_cost":{"amount":"125","currency":"USD"}}
ALTER TABLE sessions ADD COLUMN budget jsonb;

-- +goose Down

ALTER TABLE sessions DROP COLUMN budget;
