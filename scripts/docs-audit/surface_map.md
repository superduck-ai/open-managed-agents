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
/messages -> docs/design/be/messages-proxy.md
/models -> internal
/organizations -> internal
/sessions -> gated:needs-design-doc
/skills -> gated:needs-design-doc
/mcp/vault-auth/start -> gated:needs-design-doc
/vaults -> gated:needs-design-doc
/webhooks -> gated:needs-design-doc

## Packages -> design docs

admin -> internal
agents -> gated:needs-design-doc
agentsnapshot -> internal
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
mcpcatalogs -> docs/design/mcp-tool-catalog-discovery.md
messages -> docs/design/be/messages-proxy.md
memory -> gated:needs-design-doc
models -> internal
networkpolicy -> docs/design/be/ccrv2/upstream-proxy-and-model-runtime.md
observability -> internal
platform -> internal
platformapi -> internal
platformauth -> docs/design/be/db-platform-auth-boundaries.md
platformsession -> internal
sessions -> docs/design/be/permission-policies.md
skills -> gated:needs-design-doc
skillprewarm -> gated:needs-design-doc
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
00010_builtin_skills.sql -> gated:needs-design-doc
00011_unique_skill_display_title.sql -> internal
00012_require_skill_display_title.sql -> internal
00014_globalize_mcp_tool_catalogs.sql -> docs/design/mcp-tool-catalog-discovery.md
00015_add_code_session_model_access_tokens.sql -> docs/design/be/messages-proxy.md
00016_rename_code_session_oauth_tokens.sql -> docs/design/be/messages-proxy.md
00017_remove_code_session_credential_expiry.sql -> docs/design/be/messages-proxy.md

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

## API subroutes -> design docs

# Each Register*Routes entry point = one HTTP resource contributed by a package.
# Most map to the platform/workbench boundary doc; domain-specific ones map to
# their area design doc; unmapped resources that need their own doc use gated:.
codesessions.RegisterV1Routes -> docs/design/be/ccrv2/upstream-proxy-and-model-runtime.md
codesessions.RegisterV2Routes -> docs/design/be/ccrv2/upstream-proxy-and-model-runtime.md
files.RegisterPlatformRoutes -> gated:needs-design-doc
mcpcatalogs.RegisterRoutes -> docs/design/mcp-tool-catalog-discovery.md
platformapi.RegisterConsoleOrganizationAPIKeyRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterConsoleOrganizationAdminRequestRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterConsoleOrganizationInviteRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterConsoleOrganizationMemberRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterConsoleOrganizationWorkspaceRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterDirectoryRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationAnalyticsRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationBillingRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationExperienceRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationOAuthEnvironmentRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationOnboardingRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationProfileRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationProxyRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationRootRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterOrganizationSSORoutes -> docs/design/be/db-platform-auth-boundaries.md
platformapi.RegisterPlatformAccountRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterPlatformBillingRoutes -> docs/design/be/http-platform-workbench-boundaries.md
platformapi.RegisterPlatformEmailLoginRoutes -> docs/design/be/db-platform-auth-boundaries.md
platformapi.RegisterPlatformPrivacyConsentRoutes -> docs/design/be/http-platform-workbench-boundaries.md
workbench.RegisterOrgWorkbenchRoutes -> docs/design/be/http-platform-workbench-boundaries.md

## Event contracts -> design docs

# Event-type strings from internal/managedagentsevents/events.go. These are the
# managed-agent event contract consumed by the FE session timeline. Session
# status / thread lifecycle events map to the lane-timeline design; agent/tool/
# span/user/system events map to the same FE contract doc until a dedicated
# events design doc exists.
agent.custom_tool_use -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
agent.mcp_tool_result -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
agent.mcp_tool_use -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
agent.message -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
agent.thinking -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
agent.thread_context_compacted -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
agent.thread_message_received -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
agent.thread_message_sent -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
agent.tool_result -> docs/design/fe/sessions/session-tool-call-display.md
agent.tool_use -> docs/design/fe/sessions/session-tool-call-display.md
session.deleted -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.error -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.idled -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.requires_action -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.running -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.status_idle -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.status_idled -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.status_rescheduled -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.status_run_started -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.status_running -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.status_terminated -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.thread_created -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.thread_idled -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.thread_status_idle -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.thread_status_rescheduled -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.thread_status_running -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.thread_status_terminated -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.thread_terminated -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
session.updated -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
span.model_request_end -> gated:needs-design-doc
span.model_request_start -> gated:needs-design-doc
span.outcome_evaluation_end -> gated:needs-design-doc
span.outcome_evaluation_ongoing -> gated:needs-design-doc
span.outcome_evaluation_start -> gated:needs-design-doc
system.message -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
user.custom_tool_result -> docs/design/fe/sessions/session-tool-call-display.md
user.define_outcome -> gated:needs-design-doc
user.interrupt -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
user.message -> docs/design/fe/sessions/session-detail-lane-timeline-design.md
user.tool_confirmation -> docs/design/fe/sessions/session-tool-permission-confirmation.md
user.tool_result -> docs/design/fe/sessions/session-tool-call-display.md

## Auth middleware -> design docs

# Middleware surfaces from internal/api/server.go. Infra middleware (requestID,
# recover) is internal; auth middleware maps to permission/auth boundary docs.
optionalPlatformAuthMiddleware -> docs/design/be/db-platform-auth-boundaries.md
platformAuthMiddleware -> docs/design/be/db-platform-auth-boundaries.md
recoverMiddleware -> internal
requestIDMiddleware -> internal
serviceAuthMiddleware -> docs/design/be/permission-policies.md
v1AuthMiddleware -> docs/design/be/auth-credential-routing.md

## Unlisted design docs

docs/design/be/ccrv2/claude_code-permission-modes.md
docs/design/be/ccrv2/otlp-metrics-api.md
docs/design/be/ccrv2/worker-get-api.md
docs/design/be/ccrv2/worker-heartbeat-server-implementation.md
docs/design/be/managed-agent-claude-code-permission-bridge.md
docs/design/be/docs-sync-design-doc-audit.md
docs/design/fe/sessions/session-tool-call-display.md
docs/design/fe/sessions/session-tool-permission-confirmation.md
