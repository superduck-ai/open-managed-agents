{{define "turns"}}turns AS (
  SELECT dsh_session_id AS sid, MAX(oma_session_id) AS session_id, MAX(CAST(oma_agent_version AS BIGINT)) AS agent_version, dsh_turn AS turn,
    MIN(CASE WHEN dsh_event_type = 'turn/start' THEN CAST(dsh_event_time AS BIGINT) END) AS started_at,
    MAX(CASE WHEN dsh_event_type = 'turn/end' THEN CAST(dsh_event_time AS BIGINT) END) AS ended_at,
    MAX(CASE WHEN dsh_event_type = 'turn/end' THEN dsh_turn_end_reason END) AS reason,
    MAX(CASE WHEN dsh_event_type = 'turn/end' THEN dsh_error_code END) AS error_code
  {{.Source}} AND dsh_event_type IN ('turn/start', 'turn/end') AND dsh_turn > 0
  GROUP BY dsh_session_id, dsh_turn
), complete AS (
  SELECT sid, session_id, agent_version, turn, ended_at, CAST(ended_at - started_at AS DOUBLE) AS duration_ms, reason, error_code, starts_with(sid, 'sthr_') AS child
  FROM turns WHERE started_at IS NOT NULL AND ended_at IS NOT NULL AND ended_at >= started_at AND ended_at >= {{.From}}
){{end}}

{{define "requests"}}requests AS (
  SELECT dsh_session_id AS sid, MAX(oma_session_id) AS session_id, MAX(CAST(dsh_event_time AS BIGINT)) AS ended_at,
    MAX(oma_model_request_started_at) AS started_at, MAX(oma_model_request_first_token_at) AS first_token_at,
    MAX(gen_ai_request_model) AS model, MAX(dsh_model_request_status) AS status, MAX(dsh_error_code) AS error_code,
    MAX(dsh_turn) AS turn, MAX(dsh_step) AS step, MAX(CAST(dsh_event_seq AS BIGINT)) AS seq,
    MAX(COALESCE(gen_ai_usage_input_tokens, 0)) AS input_tokens, MAX(COALESCE(gen_ai_usage_output_tokens, 0)) AS output_tokens,
    MAX(COALESCE(gen_ai_usage_cache_read_input_tokens, 0)) AS cache_read_tokens, MAX(COALESCE(gen_ai_usage_cache_creation_input_tokens, 0)) AS cache_write_tokens,
    MAX(COALESCE(gen_ai_usage_reasoning_output_tokens, 0)) AS reasoning_tokens
  {{.Source}} AND dsh_event_type IN ('assistant/message', 'assistant/attempt'){{if .Model}} AND gen_ai_request_model = {{q .Model}}{{end}}
  GROUP BY dsh_session_id, CAST(dsh_event_seq AS BIGINT)
), timed AS (
  SELECT sid, session_id, ended_at, model, status, error_code, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens + output_tokens + cache_read_tokens + cache_write_tokens AS tokens,
    CASE WHEN started_at > 0 THEN CAST(ended_at - started_at AS DOUBLE) END AS duration_ms,
    CASE WHEN started_at > 0 AND first_token_at >= started_at THEN CAST(first_token_at - started_at AS DOUBLE) END AS ttft_ms,
    CASE WHEN turn > 0 AND step > 0 THEN concat(sid, ':', CAST(turn AS VARCHAR), ':', CAST(step AS VARCHAR)) ELSE concat(sid, '#', CAST(seq AS VARCHAR)) END AS attempt_key
  FROM requests WHERE ended_at >= {{.From}}
){{end}}

{{define "calls"}}calls AS (
  SELECT MAX(CASE WHEN dsh_event_type = 'tool/call' THEN dsh_tool_name END) AS name,
    MIN(CASE WHEN dsh_event_type = 'tool/call' THEN CAST(dsh_event_time AS BIGINT) END) AS called_at,
    MAX(CASE WHEN dsh_event_type = 'tool/result' THEN CAST(dsh_event_time AS BIGINT) END) AS result_at,
    MAX(CASE WHEN dsh_event_type = 'tool/result' AND dsh_tool_is_error THEN 1 ELSE 0 END) AS failed,
    MAX(CASE WHEN dsh_event_type = 'tool/result' AND dsh_tool_is_error THEN dsh_error_code END) AS error_code
  {{.Source}} AND dsh_event_type IN ('tool/call', 'tool/result') AND dsh_tool_call_id != ''
  GROUP BY dsh_session_id, dsh_tool_call_id
), done AS (
  SELECT name, result_at AS ended_at, CAST(result_at - called_at AS DOUBLE) AS duration_ms, failed, error_code
  FROM calls WHERE name IS NOT NULL AND name != '' AND called_at IS NOT NULL AND result_at IS NOT NULL AND result_at >= called_at AND result_at >= {{.From}}{{if .Tool}} AND name = {{q .Tool}}{{end}}
){{end}}

{{define "sessions_summary"}}SELECT
  COUNT(DISTINCT CASE WHEN CAST(dsh_event_time AS BIGINT) >= {{.Start}} THEN oma_session_id END) AS active_sessions,
  COUNT(DISTINCT CASE WHEN CAST(dsh_event_time AS BIGINT) < {{.Start}} THEN oma_session_id END) AS active_sessions_previous
{{.Source}}
{{end}}

{{define "turns_summary"}}WITH {{template "turns" .}}
SELECT
  COALESCE(SUM(CASE WHEN ended_at >= {{.Start}} THEN 1 ELSE 0 END), 0) AS interactions,
  COALESCE(SUM(CASE WHEN ended_at < {{.Start}} THEN 1 ELSE 0 END), 0) AS interactions_previous,
  approx_percentile_cont(CASE WHEN ended_at >= {{.Start}} THEN duration_ms END, 0.5) AS duration_p50,
  approx_percentile_cont(CASE WHEN ended_at < {{.Start}} THEN duration_ms END, 0.5) AS duration_p50_previous,
  approx_percentile_cont(CASE WHEN ended_at >= {{.Start}} THEN duration_ms END, 0.95) AS duration_p95
FROM complete WHERE NOT child
{{end}}

{{define "requests_summary"}}WITH {{template "requests" .}}, current_requests AS (
  SELECT * FROM timed WHERE ended_at >= {{.Start}}
), previous_requests AS (
  SELECT * FROM timed WHERE ended_at < {{.Start}}
), current_stats AS (
  SELECT COUNT(*) AS requests,
    SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) * 100.0 / NULLIF(COUNT(*), 0) AS failure_rate,
    (COUNT(*) - COUNT(DISTINCT attempt_key)) * 100.0 / NULLIF(COUNT(*), 0) AS retry_rate,
    approx_percentile_cont(duration_ms, 0.5) AS duration_p50,
    approx_percentile_cont(duration_ms, 0.95) AS duration_p95,
    AVG(CASE WHEN status = 'ok' THEN ttft_ms END) AS ttft_avg,
    approx_percentile_cont(CASE WHEN status = 'ok' THEN ttft_ms END, 0.95) AS ttft_p95,
    COALESCE(SUM(tokens), 0) AS tokens,
    SUM(cache_read_tokens) * 100.0 / NULLIF(SUM(input_tokens + cache_read_tokens + cache_write_tokens), 0) AS cache_hit_rate
  FROM current_requests
), previous_stats AS (
  SELECT COUNT(*) AS requests_previous,
    SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) * 100.0 / NULLIF(COUNT(*), 0) AS failure_rate_previous,
    (COUNT(*) - COUNT(DISTINCT attempt_key)) * 100.0 / NULLIF(COUNT(*), 0) AS retry_rate_previous,
    approx_percentile_cont(duration_ms, 0.5) AS duration_p50_previous,
    AVG(CASE WHEN status = 'ok' THEN ttft_ms END) AS ttft_avg_previous,
    COALESCE(SUM(tokens), 0) AS tokens_previous,
    SUM(cache_read_tokens) * 100.0 / NULLIF(SUM(input_tokens + cache_read_tokens + cache_write_tokens), 0) AS cache_hit_rate_previous
  FROM previous_requests
)
SELECT * FROM current_stats CROSS JOIN previous_stats
{{end}}

{{define "turns_per_session"}}WITH {{template "turns" .}}, per_session AS (
  SELECT session_id, CAST(COUNT(*) AS DOUBLE) AS turns FROM complete WHERE NOT child GROUP BY session_id
)
SELECT approx_percentile_cont(turns, 0.5) AS p50, approx_percentile_cont(turns, 0.9) AS p90, approx_percentile_cont(turns, 0.95) AS p95 FROM per_session
{{end}}

{{define "tokens_per_session"}}WITH {{template "requests" .}}, per_session AS (
  SELECT session_id, CAST(SUM(tokens) AS DOUBLE) AS tokens FROM timed GROUP BY session_id
)
SELECT approx_percentile_cont(tokens, 0.5) AS p50, approx_percentile_cont(tokens, 0.9) AS p90, approx_percentile_cont(tokens, 0.95) AS p95 FROM per_session
{{end}}

{{define "session_trend"}}SELECT (CAST(dsh_event_time AS BIGINT) / {{.Interval}}) * {{.Interval}} AS bucket_start, 'sessions' AS series, CAST(COUNT(DISTINCT oma_session_id) AS DOUBLE) AS value
{{.Source}}
GROUP BY (CAST(dsh_event_time AS BIGINT) / {{.Interval}}) * {{.Interval}}
{{end}}

{{define "request_trend"}}WITH {{template "requests" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, CASE WHEN status = 'error' THEN 'failed' ELSE 'succeeded' END AS series, CAST(COUNT(*) AS DOUBLE) AS value
FROM timed GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}, CASE WHEN status = 'error' THEN 'failed' ELSE 'succeeded' END
{{end}}

{{define "token_trend"}}WITH {{template "requests" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'input' AS series, CAST(SUM(input_tokens) AS DOUBLE) AS value FROM timed GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
UNION ALL
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'output' AS series, CAST(SUM(output_tokens) AS DOUBLE) AS value FROM timed GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
UNION ALL
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'cache_read' AS series, CAST(SUM(cache_read_tokens) AS DOUBLE) AS value FROM timed GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
UNION ALL
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'cache_write' AS series, CAST(SUM(cache_write_tokens) AS DOUBLE) AS value FROM timed GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
{{end}}

{{define "turn_duration_trend"}}WITH {{template "turns" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'p50' AS series, approx_percentile_cont(duration_ms, 0.5) AS value FROM complete WHERE NOT child GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
UNION ALL
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'p95' AS series, approx_percentile_cont(duration_ms, 0.95) AS value FROM complete WHERE NOT child GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
{{end}}

{{define "model_tokens"}}WITH {{template "requests" .}}
SELECT model AS name, CAST(SUM(tokens) AS DOUBLE) AS value FROM timed WHERE model != '' GROUP BY model ORDER BY value DESC LIMIT 20
{{end}}

{{define "tool_top"}}WITH {{template "calls" .}}
SELECT name, CAST(COUNT(*) AS DOUBLE) AS value FROM done GROUP BY name ORDER BY value DESC LIMIT 10
{{end}}

{{define "end_reasons"}}WITH {{template "turns" .}}
SELECT reason AS name, CAST(COUNT(*) AS DOUBLE) AS value FROM complete WHERE NOT child AND reason IS NOT NULL AND reason != '' GROUP BY reason ORDER BY value DESC
{{end}}

{{define "versions"}}WITH {{template "turns" .}}
SELECT agent_version AS version, CAST(COUNT(*) AS DOUBLE) AS interactions, CAST(SUM(CASE WHEN reason = 'error' THEN 1 ELSE 0 END) AS DOUBLE) AS errors,
  approx_percentile_cont(duration_ms, 0.5) AS p50_ms, approx_percentile_cont(duration_ms, 0.95) AS p95_ms
FROM complete WHERE NOT child GROUP BY agent_version ORDER BY version DESC LIMIT 20
{{end}}

{{define "model_names"}}SELECT DISTINCT gen_ai_request_model AS name {{.Source}} AND dsh_event_type IN ('assistant/message', 'assistant/attempt') AND gen_ai_request_model != '' ORDER BY name LIMIT 50
{{end}}

{{define "tokens_per_request_trend"}}WITH {{template "requests" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'tokens' AS series, AVG(CAST(tokens AS DOUBLE)) AS value FROM timed GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
{{end}}

{{define "model_request_trend"}}WITH {{template "requests" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, model AS series, CAST(COUNT(*) AS DOUBLE) AS value FROM timed WHERE model != '' GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}, model
{{end}}

{{define "model_latency_trend"}}WITH {{template "requests" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'p50' AS series, approx_percentile_cont(duration_ms, 0.5) AS value FROM timed WHERE duration_ms IS NOT NULL GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
UNION ALL
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, 'p95' AS series, approx_percentile_cont(duration_ms, 0.95) AS value FROM timed WHERE duration_ms IS NOT NULL GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}
{{end}}

{{define "model_error_trend"}}WITH {{template "requests" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, CASE WHEN error_code = '' OR error_code IS NULL THEN 'unknown' ELSE error_code END AS series, CAST(COUNT(*) AS DOUBLE) AS value
FROM timed WHERE status = 'error' GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}, CASE WHEN error_code = '' OR error_code IS NULL THEN 'unknown' ELSE error_code END
{{end}}

{{define "model_table"}}WITH {{template "requests" .}}
SELECT model, CAST(COUNT(*) AS DOUBLE) AS requests,
  CAST(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) AS DOUBLE) AS failures,
  SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) * 100.0 / NULLIF(COUNT(*), 0) AS failure_rate,
  CAST(COUNT(*) - COUNT(DISTINCT attempt_key) AS DOUBLE) AS retries,
  CAST(SUM(input_tokens) AS DOUBLE) AS input_tokens, CAST(SUM(output_tokens) AS DOUBLE) AS output_tokens,
  CAST(SUM(cache_read_tokens) AS DOUBLE) AS cache_read_tokens, CAST(SUM(cache_write_tokens) AS DOUBLE) AS cache_write_tokens,
  CAST(SUM(reasoning_tokens) AS DOUBLE) AS reasoning_tokens, CAST(SUM(tokens) AS DOUBLE) AS tokens,
  SUM(cache_read_tokens) * 100.0 / NULLIF(SUM(input_tokens + cache_read_tokens + cache_write_tokens), 0) AS cache_hit_rate,
  approx_percentile_cont(duration_ms, 0.5) AS p50_ms, approx_percentile_cont(duration_ms, 0.95) AS p95_ms, approx_percentile_cont(duration_ms, 0.99) AS p99_ms,
  approx_percentile_cont(CASE WHEN status = 'ok' THEN ttft_ms END, 0.5) AS ttft_p50_ms, approx_percentile_cont(CASE WHEN status = 'ok' THEN ttft_ms END, 0.95) AS ttft_p95_ms
FROM timed WHERE model != '' GROUP BY model ORDER BY requests DESC LIMIT 20
{{end}}

{{define "model_error_codes"}}WITH {{template "requests" .}}
SELECT CASE WHEN error_code = '' OR error_code IS NULL THEN 'unknown' ELSE error_code END AS name, CAST(COUNT(*) AS DOUBLE) AS value
FROM timed WHERE status = 'error' GROUP BY CASE WHEN error_code = '' OR error_code IS NULL THEN 'unknown' ELSE error_code END ORDER BY value DESC LIMIT 20
{{end}}

{{define "tool_names"}}SELECT DISTINCT dsh_tool_name AS name {{.Source}} AND dsh_event_type = 'tool/call' AND dsh_tool_name != '' ORDER BY name LIMIT 100
{{end}}

{{define "tools_summary"}}WITH {{template "calls" .}}, current_calls AS (
  SELECT * FROM done WHERE ended_at >= {{.Start}}
), previous_calls AS (
  SELECT * FROM done WHERE ended_at < {{.Start}}
), current_stats AS (
  SELECT COUNT(*) AS calls, COALESCE(SUM(failed), 0) AS errors, SUM(failed) * 100.0 / NULLIF(COUNT(*), 0) AS error_rate,
    AVG(duration_ms) AS duration_avg, approx_percentile_cont(duration_ms, 0.95) AS duration_p95, SUM(duration_ms) AS total_ms
  FROM current_calls
), previous_stats AS (
  SELECT COUNT(*) AS calls_previous, COALESCE(SUM(failed), 0) AS errors_previous, SUM(failed) * 100.0 / NULLIF(COUNT(*), 0) AS error_rate_previous,
    AVG(duration_ms) AS duration_avg_previous, SUM(duration_ms) AS total_ms_previous
  FROM previous_calls
)
SELECT * FROM current_stats CROSS JOIN previous_stats
{{end}}

{{define "tool_call_trend"}}WITH {{template "calls" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, name AS series, CAST(COUNT(*) AS DOUBLE) AS value FROM done GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}, name
{{end}}

{{define "tool_latency_trend"}}WITH {{template "calls" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, name AS series, approx_percentile_cont(duration_ms, 0.95) AS value FROM done GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}, name
{{end}}

{{define "tool_error_trend"}}WITH {{template "calls" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, name AS series, CAST(SUM(failed) AS DOUBLE) AS value FROM done GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}, name
{{end}}

{{define "tool_error_codes"}}WITH {{template "calls" .}}
SELECT CASE WHEN error_code = '' OR error_code IS NULL THEN 'unknown' ELSE error_code END AS name, CAST(COUNT(*) AS DOUBLE) AS value
FROM done WHERE failed = 1 GROUP BY CASE WHEN error_code = '' OR error_code IS NULL THEN 'unknown' ELSE error_code END ORDER BY value DESC LIMIT 20
{{end}}

{{define "tool_table"}}WITH {{template "calls" .}}, totals AS (
  SELECT SUM(duration_ms) AS all_ms FROM done
)
SELECT name AS tool, CASE WHEN starts_with(name, 'mcp__') THEN split_part(name, '__', 2) ELSE '' END AS server,
  CAST(COUNT(*) AS DOUBLE) AS calls, CAST(SUM(failed) AS DOUBLE) AS errors, SUM(failed) * 100.0 / NULLIF(COUNT(*), 0) AS error_rate,
  AVG(duration_ms) AS avg_ms, approx_percentile_cont(duration_ms, 0.95) AS p95_ms, MAX(duration_ms) AS max_ms, SUM(duration_ms) AS total_ms,
  SUM(duration_ms) * 100.0 / NULLIF(MAX(totals.all_ms), 0) AS share
FROM done CROSS JOIN totals GROUP BY name ORDER BY calls DESC LIMIT 20
{{end}}

{{define "subagent_turn_trend"}}WITH {{template "turns" .}}
SELECT (ended_at / {{.Interval}}) * {{.Interval}} AS bucket_start, CASE WHEN reason = 'error' THEN 'failed' ELSE 'succeeded' END AS series, CAST(COUNT(*) AS DOUBLE) AS value
FROM complete WHERE child GROUP BY (ended_at / {{.Interval}}) * {{.Interval}}, CASE WHEN reason = 'error' THEN 'failed' ELSE 'succeeded' END
{{end}}

{{define "subagent_threads"}}WITH {{template "turns" .}}, {{template "requests" .}}, thread_turns AS (
  SELECT sid, COUNT(*) AS turns, SUM(CASE WHEN reason = 'error' THEN 1 ELSE 0 END) AS failures, SUM(duration_ms) AS total_ms, MAX(duration_ms) AS max_ms
  FROM complete WHERE child GROUP BY sid
), thread_tokens AS (
  SELECT sid, SUM(tokens) AS tokens FROM timed WHERE starts_with(sid, 'sthr_') GROUP BY sid
)
SELECT thread_turns.sid AS thread_id, thread_turns.turns, thread_turns.failures, thread_turns.total_ms, thread_turns.max_ms, COALESCE(thread_tokens.tokens, 0) AS tokens
FROM thread_turns LEFT JOIN thread_tokens ON thread_turns.sid = thread_tokens.sid ORDER BY thread_turns.turns DESC LIMIT 500
{{end}}
