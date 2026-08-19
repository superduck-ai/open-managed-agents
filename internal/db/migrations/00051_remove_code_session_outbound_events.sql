-- +goose Up

-- Tool confirmations used to recover the original control request from the
-- private outbound log. Preserve the only fields that flow still needs on the
-- corresponding public tool event before removing the log.
with permission_requests as (
    select distinct on (
        code_session.workspace_uuid,
        code_session.session_uuid,
        outbound_event.payload->'request'->>'tool_use_id'
    )
        code_session.workspace_uuid,
        code_session.session_uuid,
        outbound_event.payload->'request'->>'tool_use_id' as tool_use_id,
        coalesce(
            nullif(btrim(outbound_event.request_id), ''),
            nullif(btrim(outbound_event.payload->>'request_id'), '')
        ) as request_id
    from code_session_outbound_events outbound_event
    join code_sessions code_session
      on code_session.uuid = outbound_event.code_session_uuid
     and code_session.workspace_uuid = outbound_event.workspace_uuid
    where outbound_event.deleted_at is null
      and outbound_event.event_type = 'control_request'
      and outbound_event.event_subtype = 'can_use_tool'
      and nullif(btrim(outbound_event.payload->'request'->>'tool_use_id'), '') is not null
      and coalesce(
          nullif(btrim(outbound_event.request_id), ''),
          nullif(btrim(outbound_event.payload->>'request_id'), '')
      ) is not null
    order by
        code_session.workspace_uuid,
        code_session.session_uuid,
        outbound_event.payload->'request'->>'tool_use_id',
        outbound_event.sequence_num desc
)
update session_events session_event
set payload = jsonb_set(
    session_event.payload,
    '{request_id}',
    to_jsonb(permission_request.request_id),
    true
)
from permission_requests permission_request
where session_event.workspace_uuid = permission_request.workspace_uuid
  and session_event.session_uuid = permission_request.session_uuid
  and session_event.event_type in ('agent.tool_use', 'agent.mcp_tool_use')
  and session_event.payload->>'tool_use_id' = permission_request.tool_use_id
  and nullif(btrim(session_event.payload->>'request_id'), '') is null;

drop table code_session_outbound_events;

alter table code_sessions
    drop column last_outbound_sequence_num;

-- +goose Down

-- Irreversible: the removed diagnostic event history cannot be reconstructed.
select 1;
