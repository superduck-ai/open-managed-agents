-- +goose Up
ALTER TABLE users ADD COLUMN registration_identity boolean NOT NULL DEFAULT false;

-- 不依据名称或角色推断历史注册来源，旧身份在登录时回退到来源组织。

-- +goose Down
ALTER TABLE users DROP COLUMN registration_identity;
