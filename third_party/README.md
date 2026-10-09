# Crush source module

`crush/` contains the complete tracked source snapshot supplied from the local
Crush checkout at revision `140e8cb9707faa6a68d87d0ecc3a85b9c65e25d5`.
`source-manifest.json` records the original source hashes before local patches.
The original license and notice files remain in the snapshot.

Local commit hygiene also normalizes non-golden file endings and Go formatting,
refreshes the nested module checksums, and keeps embedded diff fixture spaces
through explicit string concatenation. TUI golden snapshots retain their exact
original bytes. An upstream command-menu nil dereference is guarded so the
snapshot passes its own static analysis configuration.

OMA replaces `github.com/charmbracelet/crush` with this local module. Fantasy is
an ordinary, unmodified Go dependency pinned to `charm.land/fantasy v0.45.1`.
There is no local Fantasy source module or replacement.

## Embedded entry point

`runtime.Engine.Run` calls Crush's actual `internal/agent.SessionAgent.Run`.
Crush owns prompt preparation, tool validation and repair, streaming callbacks,
model continuation, token accounting and repeated-tool detection. The CLI and
TUI remain in the source snapshot but are not instantiated by OMA. Host tools,
SQLite persistence and the global MCP registry are not used by host Sessions.

The public bridge supplies session-scoped in-memory implementations of Crush's
message and session interfaces. This projection lasts for one accepted input.
OMA's fenced private history remains authoritative. Restored Fantasy messages
retain provider metadata and thinking signatures through `Message.ModelHistory`.
Crush still applies its original tool-result adjacency and media preparation.

The host options added to SessionAgent disable background title generation and
the built-in todo reminder, isolate the global MCP registry, inject streaming
callbacks, allow attachment-only inputs and preserve unresolved tool calls on
failure. Upstream default
behavior remains unchanged when these options are absent.

## Persistence and events

The bridge uses Fantasy's public `OnChunk`, `OnToolCall`, `OnToolResult` and
`OnStepFinish` callbacks. All validated calls are collected and the assistant
response is persisted before Fantasy dispatches any client tool. A completed
response without tools is persisted at step completion. Incomplete failed streams
are not persisted as completed assistant responses. Tool results are persisted
before the next model request.

Callback failures retain their original cause and cancel the run. The model
adapter and tool adapter check that failure before starting new work. Host tools
run sequentially so result persistence completes before another tool starts.
Ordinary tool errors become model-visible error responses. An unknown outcome
returns `runtime.ExecutionError`, cancels the run and leaves the recorded call
unresolved. No fabricated result is published. OMA owns reconciliation and
blocks automatic replay of dispatched calls.

`runtime.SchemaTool` provides the original MCP JSON schema at the serialization
boundary. The model adapter uses the official Anthropic `ProviderOptions.ExtraBody`
API to send complete tool definitions, including definitions, required arrays
and additional-properties constraints. It preserves existing thinking and other
provider options. Fantasy itself requires no patches.

OMA owns durable input delivery, ACK, tenant scope, permissions, cross-instance
leases, Worker epochs and public events. Event identity includes the accepted
input, step and attempt. The bridge is not a second durable queue or database.

## Current scope and verification

Host Sessions use injected sandbox MCP tools. Automatic summarization is inactive
because the host model contract does not supply a context-window size. Built-in
child Agent orchestration, host filesystem tools, Crush CLI/TUI and title updates
are not enabled by this integration. No memory benchmark has been recorded.

Root `go test ./...` does not enter nested modules. `just agent-runtime-test`
explicitly tests the bridge and Crush's original prompt/loop tests in addition to
OMA's adapters. Failure tests cover persistence, callbacks, unresolved outcomes,
cancellation, continuation history and provider signatures. Real public startup,
approvals, MCP and SSE use `just verify-be chat host`; the upstream responses are
scripted in that scenario. Real models and cloud allocation are separate checks.
