-- +goose Up
-- session.usage / sessions.usage keep only active_milliseconds (stored).
-- Stop overlaying live-projected active_seconds onto usage; stats still use it.

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION oma_session_activity_projection(activity_workspace uuid, activity_session uuid, activity_thread text, activity_as_of timestamptz, activity_usage jsonb, activity_stats jsonb)
RETURNS jsonb LANGUAGE sql STABLE AS $$
WITH metrics AS MATERIALIZED (
 SELECT oma_session_activity_metrics(activity_workspace,activity_session,activity_thread,activity_as_of) AS value
)
SELECT jsonb_build_object(
 'usage', COALESCE(activity_usage,'{}') - 'active_seconds',
 'stats', COALESCE(activity_stats,'{}') || jsonb_build_object('active_seconds',value->'stats_active_seconds'))
FROM metrics
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION oma_session_activity_projection(activity_workspace uuid, activity_session uuid, activity_thread text, activity_as_of timestamptz, activity_usage jsonb, activity_stats jsonb)
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
