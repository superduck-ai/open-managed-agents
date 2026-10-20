-- +goose Up
CREATE INDEX IF NOT EXISTS session_activity_status_idx ON session_events (workspace_uuid, session_uuid, (payload->>'session_thread_id'), id)
WHERE deleted_at IS NULL AND event_type IN ('session.thread_status_running','session.thread_status_idle','session.thread_status_rescheduled','session.thread_status_terminated');

-- +goose StatementBegin
CREATE FUNCTION oma_session_activity_metrics(activity_workspace uuid, activity_session uuid, activity_thread text, activity_as_of timestamptz)
RETURNS jsonb LANGUAGE sql STABLE AS $$
WITH scoped AS NOT MATERIALIZED (
 SELECT event.id, event.payload->>'session_thread_id' AS thread_id,
        event.event_type, event.processed_at,
        thread.status AS thread_status, thread.updated_at AS thread_updated_at,
        thread.archived_at, thread.deleted_at,
        session.status AS session_status, session.updated_at AS session_updated_at,
        session.deleted_at AS session_deleted_at
 FROM session_events AS event
 JOIN session_threads AS thread ON thread.workspace_uuid=event.workspace_uuid AND thread.session_uuid=event.session_uuid
      AND thread.external_id=event.thread_external_id
 JOIN sessions AS session ON session.uuid=event.session_uuid AND session.workspace_uuid=event.workspace_uuid
 WHERE event.workspace_uuid=activity_workspace AND event.session_uuid=activity_session
   AND event.deleted_at IS NULL
   AND event.event_type IN ('session.thread_status_running','session.thread_status_idle','session.thread_status_rescheduled','session.thread_status_terminated')
   AND event.payload->>'session_thread_id'=event.thread_external_id
), canonical AS (
 SELECT * FROM scoped WHERE activity_thread IS NULL
 UNION ALL
 SELECT * FROM scoped WHERE activity_thread IS NOT NULL AND thread_id=activity_thread
), transitions AS (
 SELECT *, lead(processed_at) OVER (PARTITION BY thread_id ORDER BY id) AS next_at
 FROM canonical
), spans AS (
 SELECT processed_at AS started_at,
        LEAST(activity_as_of, GREATEST(processed_at, LEAST(
          COALESCE(next_at,activity_as_of),
          COALESCE(session_deleted_at,activity_as_of),
          CASE WHEN session_status='terminated' THEN session_updated_at ELSE activity_as_of END,
          COALESCE(deleted_at,activity_as_of),
          CASE WHEN thread_status!='running' THEN thread_updated_at ELSE activity_as_of END))) AS finished_at
 FROM transitions
 WHERE event_type='session.thread_status_running' AND processed_at <= activity_as_of
), combined AS (
 SELECT range_agg(tstzrange(started_at,finished_at,'[)')) AS periods,
        COALESCE(sum(extract(epoch FROM finished_at-started_at)),0) AS summed
 FROM spans
), unified AS (
 SELECT COALESCE(sum(extract(epoch FROM upper(period)-lower(period))),0) AS elapsed
 FROM combined CROSS JOIN LATERAL unnest(periods) AS period
)
SELECT jsonb_build_object('active_seconds',unified.elapsed,'stats_active_seconds',combined.summed)
FROM unified CROSS JOIN combined
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION oma_session_activity_projection(activity_workspace uuid, activity_session uuid, activity_thread text, activity_as_of timestamptz, activity_usage jsonb, activity_stats jsonb)
RETURNS jsonb LANGUAGE sql STABLE AS $$
WITH metrics AS MATERIALIZED (
 SELECT oma_session_activity_metrics(activity_workspace,activity_session,activity_thread,activity_as_of) AS value
)
SELECT jsonb_build_object(
 'usage',COALESCE(activity_usage,'{}') || jsonb_build_object('active_seconds',value->'active_seconds'),
 'stats',COALESCE(activity_stats,'{}') || jsonb_build_object('active_seconds',value->'stats_active_seconds'))
FROM metrics
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION oma_session_activity_projection(uuid,uuid,text,timestamptz,jsonb,jsonb);
DROP FUNCTION oma_session_activity_metrics(uuid,uuid,text,timestamptz);
DROP INDEX session_activity_status_idx;
