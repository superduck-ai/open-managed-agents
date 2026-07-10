# Design Doc Surface Map

Curated mapping of code surfaces to `docs/design/` pages.

Format: `SurfaceID -> docs/design/path/to/page.md`

Sentinels: `internal` | `gated:<reason>`


## API mounts -> design docs

/agents -> gated:needs-design-doc
/agents:search -> gated:needs-design-doc
/deployment_runs -> gated:needs-design-doc
/deployments -> gated:needs-design-doc
/environments -> gated:needs-design-doc
/files -> gated:needs-design-doc
/healthz -> internal
/memory_stores -> gated:needs-design-doc
/messages/batches -> gated:needs-design-doc
/models -> internal
/organizations -> internal
/sessions -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
/skills -> gated:needs-design-doc
/v1/code/sessions -> docs/design/be/ccrv2/ccr-v2-api-worker-state.md
/vaults -> gated:needs-design-doc
/webhooks -> gated:needs-design-doc

## Packages -> design docs

admin -> internal
agents -> gated:needs-design-doc
api -> internal
auth -> internal
batches -> gated:needs-design-doc
cleanup -> internal
codesessions -> docs/design/be/ccrv2/ccr-v2-api-worker-state.md
config -> internal
db -> internal
deployments -> gated:needs-design-doc
environments -> gated:needs-design-doc
files -> gated:needs-design-doc
httpapi -> internal
ids -> internal
managedagentsevents -> gated:needs-design-doc
memory -> gated:needs-design-doc
models -> internal
observability -> internal
platform -> internal
platformapi -> internal
platformauth -> docs/design/be/db-platform-auth-boundaries.md
platformsession -> internal
sessions -> docs/design/be/permission-policies.md
skills -> gated:needs-design-doc
storage -> internal
vaults -> gated:needs-design-doc
webhooks -> gated:needs-design-doc
workbench -> docs/design/be/http-platform-workbench-boundaries.md

## Migrations -> design docs

00001_init.sql -> internal
00002_add_mcp_oauth_flows.sql -> gated:needs-design-doc
00003_add_code_session_worker_epoch.sql -> docs/design/be/ccrv2/ccr-v2-epoch-design.md
00004_add_code_session_worker_state.sql -> docs/design/be/ccrv2/ccr-v2-api-worker-state.md
00005_ensure_code_session_worker_state.sql -> docs/design/be/ccrv2/ccr-v2-api-worker-state.md
00006_ensure_code_session_worker_epoch_default.sql -> docs/design/be/ccrv2/ccr-v2-epoch-design.md
00007_add_code_session_inbound_delivery_ack.sql -> docs/design/be/ccrv2/ccr-v2-api-worker-events-delivery.md
00008_add_code_session_outbound_event_ephemeral.sql -> docs/design/be/ccrv2/ccr-v2-worker-events-delivery-backend-design.md
00009_add_code_session_internal_events.sql -> docs/design/be/ccrv2/ccr-v2-api-worker-internal-events.md

## FE routes -> design docs

/ -> internal
agents -> gated:needs-design-doc
agents/$agentId -> gated:needs-design-doc
api-keys -> internal
batches -> gated:needs-design-doc
billing -> internal
caching -> internal
claude-code/settings -> internal
claude-code/usage -> internal
cost -> internal
credential-vaults -> gated:needs-design-doc
dashboard -> internal
deployments -> gated:needs-design-doc
dreams -> gated:needs-design-doc
environments -> gated:needs-design-doc
files -> gated:needs-design-doc
limits -> internal
login -> internal
logs -> internal
mcp-tunnels -> gated:needs-design-doc
members -> internal
memory-stores -> gated:needs-design-doc
playground -> gated:needs-design-doc
privacy-controls -> internal
quickstart -> gated:needs-design-doc
rate-limits -> internal
security -> internal
service-accounts -> internal
sessions -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
settings/$setting -> internal
settings/limits -> internal
settings/members -> internal
settings/organization -> internal
settings/service-accounts -> internal
settings/workspaces/$workspaceId/keys -> internal
settings/workspaces/$workspaceId/webhooks -> internal
skills -> gated:needs-design-doc
skills/$skillId -> gated:needs-design-doc
skills/new -> gated:needs-design-doc
tags -> internal
usage -> internal
usage/cache -> internal
usage/limits -> internal
webhooks -> gated:needs-design-doc
workbench -> docs/design/be/http-platform-workbench-boundaries.md
workbench/$promptId -> docs/design/be/http-platform-workbench-boundaries.md
workspaces/$workspaceId/agent-quickstart -> gated:needs-design-doc
workspaces/$workspaceId/agents -> gated:needs-design-doc
workspaces/$workspaceId/agents/$agentId -> gated:needs-design-doc
workspaces/$workspaceId/batches -> gated:needs-design-doc
workspaces/$workspaceId/cost -> internal
workspaces/$workspaceId/deployments -> gated:needs-design-doc
workspaces/$workspaceId/deployments/$deploymentId -> gated:needs-design-doc
workspaces/$workspaceId/dreams -> gated:needs-design-doc
workspaces/$workspaceId/environments -> gated:needs-design-doc
workspaces/$workspaceId/environments/$environmentId -> gated:needs-design-doc
workspaces/$workspaceId/files -> gated:needs-design-doc
workspaces/$workspaceId/logs -> internal
workspaces/$workspaceId/memory-stores -> gated:needs-design-doc
workspaces/$workspaceId/memory-stores/$memoryStoreId -> gated:needs-design-doc
workspaces/$workspaceId/playground -> gated:needs-design-doc
workspaces/$workspaceId/sessions -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
workspaces/$workspaceId/sessions/$sessionId -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
workspaces/$workspaceId/skills -> gated:needs-design-doc
workspaces/$workspaceId/skills/$skillId -> gated:needs-design-doc
workspaces/$workspaceId/skills/new -> gated:needs-design-doc
workspaces/$workspaceId/vaults -> gated:needs-design-doc
workspaces/$workspaceId/vaults/$vaultId -> gated:needs-design-doc

## Unlisted design docs

docs/design/be/ccrv2/claude_code-permission-modes.md
docs/design/be/ccrv2/otlp-metrics-api.md
docs/design/be/ccrv2/worker-get-api.md
docs/design/be/ccrv2/worker-heartbeat-server-implementation.md
docs/design/be/managed-agent-claude-code-permission-bridge.md
docs/design/be/docs-sync-design-doc-audit.md
docs/design/fe/sessions/session-tool-call-display.md
docs/design/fe/sessions/session-tool-permission-confirmation.md
