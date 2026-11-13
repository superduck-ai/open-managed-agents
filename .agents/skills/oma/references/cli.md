# Observed ant compatibility notes

Use the original [CLI reference](../upstream/claude-api/shared/anthropic-cli.md) for installation, flags, apply, lockfiles, and runtime commands. Configure the target using [connection.md](connection.md). Tested CLI: ant 2.5.0. These observations concern the tested deployment, not all OMA instances.

## Environments

The tested instance required `?beta=true`; ant's `--beta managed-agents-2026-04-01` header did not satisfy this check. When the target returns this specific error, query environments with:

```bash
curl --fail-with-body --silent --show-error \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  "$ANTHROPIC_BASE_URL/v1/environments?beta=true&limit=20"
```

Follow next_page when present. Prefer a suitable existing environment. For a new minimal restricted environment:

```bash
curl --fail-with-body --silent --show-error \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H 'Content-Type: application/json' \
  "$ANTHROPIC_BASE_URL/v1/environments?beta=true" \
  --data '{"name":"Agent Environment","config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}}}'
```

Use the actual response ID. Adapt networking to task dependencies as upstream specifies. On this instance, manage environments through HTTP rather than adding their files to ant apply. On other instances, first check whether the original upstream commands work.

## Events

Raw HTTP SSE delivered events, but ant events stream produced no output during observation. This does not establish a root cause. For this mismatch, query event history with `ant beta:sessions:events list --session-id "$SESSION_ID" --format jsonl` and inspect the session with `ant beta:sessions retrieve`.

Bound polling by a deadline; preserve the Session ID for recovery. Paginate and deduplicate by event ID. Associate results with the submitted input and subsequent model request. A previous end_turn does not complete the current input. Handle errors, requires_action, and budget_reached as upstream describes. The skill does not include an automated waiting implementation.

## Files

File upload and deletion passed. The uploaded file had downloadable=false and its download returned HTTP 400. Do not generalize this to generated artifacts or promise downloads based on upload success. Validate the required file lifecycle for the user's task.
