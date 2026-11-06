# OMA Brain

Brain runs the DeepSeek native Agent loop outside the sandbox and communicates with OMA through DSRv1. It supports only DSRv1. `OMA_BRAIN_PROTOCOL` may be omitted or set to `dsr1`; any other value fails startup before contacting the backend.

The backend default is `code_session.brain_worker_protocol: dsr1`. Existing sessions retain their stored protocol. A session created for the removed transport needs its original Brain binary or must be replaced with a new DSR session. Changing its protocol metadata does not migrate its native history.

## Start and build

Requires Bun 1.3 or newer and an OMA backend exposing the DSRv1 contracts. The pinned DSH version is `0.2.2-alpha.2`, with Cordis `4.0.6-alpha.1`.

```sh
bun install --frozen-lockfile
bun run typecheck
bun test
bun run build
bun run build:executable
```

Run `just restart-brain` from the repository root for the current worktree's configured development environment. A standalone process requires `OMA_API_URL` and `OMA_BRAIN_CLAIM_TOKEN`. It loads `.env` relative to its working directory. The executable can run without source files or node_modules.

`bun run verify:executable` builds the executable and runs the Brain tests, including a process test that copies it into a temporary directory, loads `.env`, claims a DSR session, executes an approved sandbox tool, persists native history and settles the input.

## Runtime responsibilities

The entry creates a Cordis Context and mounts `src/dsr/worker.ts`. The host claims Code Sessions, registers the owner, starts its lease heartbeat, uses the context snapshot returned by registration, then consumes the DSR input stream. Task inputs execute serially. Controls run separately so interruption and approval remain responsive.

`src/dsr/session.ts` maps native events to OMA public events, persists native logs, manages approval requests and reports input completion. Completion remains separate from transport acknowledgement. A processed task returns scheduled; the backend independently confirms its JetStream delivery. Brain never stores or constructs NATS ACK subjects.

`src/runtime/` owns the native loop, tools, subagents, assistant output identities, user content conversion and transcript replication. `src/observability/` owns safe diagnostics and telemetry. These modules contain no legacy worker transport.

The host uses only `/v1/dsr/claims` and `/v1/dsr/sessions/{code_session_id}/...` for worker operations. Model requests continue through OMA `/v1/messages` or `/v1/chat/completions`; upstream provider credentials remain in Go. Sandbox and external MCP, attachments, web tools and telemetry use DSR-scoped endpoints and `DSR-Protocol: 1`.

## Persistence and recovery

Native DSH logs use the original Session format and continuous numeric event sequences. Epochs, control versions and pagination watermarks use decimal strings. Transcript replication confirms native tool-call records before tool dispatch and tool-result records before continuation. Root and child logs retain their identities across owner replacement.

Transcript persistence uses the OMA process-local adapter and the existing asynchronous DSR replication service. Only server-confirmed history survives a process crash; there is no local transcript WAL or JSONL cache. Attachment storage remains independent and is checked before claiming work. The host never converts historical transport envelopes into DSR logs automatically.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `OMA_API_URL` | required | OMA backend base URL |
| `OMA_BRAIN_CLAIM_TOKEN` | required | Fleet claim credential |
| `OMA_BRAIN_PROTOCOL` | `dsr1` | Only `dsr1` is supported |
| `OMA_BRAIN_MAX_SESSIONS` | `8` | Maximum concurrent owners |
| `OMA_BRAIN_CLAIM_CONCURRENCY` | `4` | Claim polling concurrency, 1–32 |
| `OMA_BRAIN_SHUTDOWN_DRAIN_MS` | `20000` | Active tool drain deadline, 0–600000 ms |
| `DSH_HOME` | `oma-brain-dsh/.dsh` | Local native runtime storage |

Heartbeat timing comes from the DSR registration response. Removed transport-specific heartbeat environment variables no longer configure the host. SIGINT/SIGTERM stop claims and drain active tool work before closing owners. The process enforces the drain deadline plus settlement/cleanup allowance; a second SIGINT forces exit.

## Tools and models

Sandbox creation belongs to the backend. The shared sandbox provider initializes lazily after tool validation, approval and the durable dispatch checkpoint. Root and child runtimes share the provider. Configuration and permissions come from the frozen registration snapshot. The backend independently enforces authorization and lifecycle checks at the gateway.

Model translation uses the Vercel AI SDK adapters under `src/llm-ai-sdk/`. Response identities and replay formats `oma-messages-v1` / `oma-chat-completions-v1` remain unchanged. Model tokens are scoped to the owning root or child request, and ambient provider credentials are not used. The official MCP SDK handles MCP session negotiation and cancellation.

## Verification and compatibility

The active tests cover DSR clients, native execution, approvals and recovery, startup and executable packaging, model translation, images, sandbox transport, telemetry and protocol-neutral runtime behavior. Tests tied to the removed bridge were archived with the original source, rather than counted as passing DSR coverage. Their full historical scenario coverage has not all been ported.

Real backend verification uses the project's explicitly invoked `verify-be` workflow. `chat brain-dsr` is the DSR scenario. Historical Brain scenarios that force the removed transport cannot use this binary unchanged. The backend's legacy endpoints and other Workers remain outside this removal.

See [DSRv1 implementation](../docs/design/be/dsrv1/implementation.md) for current contracts and integration limits. This checkout's removal backup is `tmp/brain-ccrv2-removal/brain-before.tar.gz`, with a SHA-256 receipt and archived test declarations alongside it. The backup is local and is not part of the shipped Brain.

Registration returns the complete immutable context, including predefined child MCP/permission templates. The initial configuration and model catalog persist across owner replacement; runtime credentials are rebound on each registration. Configuration changes apply to newly initialized sessions.
