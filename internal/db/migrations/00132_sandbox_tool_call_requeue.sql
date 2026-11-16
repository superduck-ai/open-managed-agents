-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION retire_sandbox_tool_calls() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state IN ('stopping','stopped','failed') THEN
  UPDATE sandbox_tool_calls SET state='pending', sandbox_uuid=NULL, lease_expires_at=NULL, updated_at=NOW()
  WHERE organization_uuid=NEW.organization_uuid AND workspace_uuid=NEW.workspace_uuid AND sandbox_uuid=NEW.uuid AND state='dispatched';
  UPDATE sandbox_tool_calls SET state='lost', error_code='executor_lost', finished_at=NOW(), updated_at=NOW()
  WHERE organization_uuid=NEW.organization_uuid AND workspace_uuid=NEW.workspace_uuid AND sandbox_uuid=NEW.uuid AND state IN ('running','cancel_requested');
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION retire_sandbox_tool_calls() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state IN ('stopping','stopped','failed') THEN
  UPDATE sandbox_tool_calls SET state='lost', finished_at=NOW(), updated_at=NOW()
  WHERE organization_uuid=NEW.organization_uuid AND workspace_uuid=NEW.workspace_uuid AND sandbox_uuid=NEW.uuid AND state IN ('dispatched','running','cancel_requested');
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
