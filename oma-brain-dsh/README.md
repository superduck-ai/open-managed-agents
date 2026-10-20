# oma-brain-dsh

Bun service embedding the published dsh agent-loop and Cordis plugins. Replaces the Rust Brain process without grok-build, a dsh CLI subprocess, or dsh kernel patches.

The application entry creates a Cordis Context and loads the OMA host plugin. The host composes dsh's original runtime packages with a separate CCRv2 bridge plugin and the OMA Messages adapter. The bridge owns protocol mapping, approval messages, remote persistence, recovery and session heartbeats. The shared session runtime owns dsh lifecycle and tool execution orchestration. The host long-polls the backend for Code Session claims, registers workers through CCRv2 in one request and supplies the sandbox MCP implementation. oma-server schedules sessions and owns sandbox creation.

## Run

Requires Bun 1.3 or newer and an OMA backend exposing the claim and CCRv2 contracts below. The pinned dsh version is `0.2.1-alpha.2`, with Cordis `4.0.5-alpha.1`.

```sh
bun install --frozen-lockfile
cp .env.example .env
# Set the backend URL and OMA_BRAIN_CLAIM_TOKEN in .env. Session credentials arrive in claim responses.
bun start
```

To build and run the bundled entry:

```sh
bun run build
bun dist/main.js
```

To package a standalone executable for the current OS and architecture, install
[just](https://github.com/casey/just) and run:

```sh
just build
./dist/oma-brain-dsh
```

The executable includes Bun and its dependencies. Deploy `dist/oma-brain-dsh`
without Bun or `node_modules`; on Windows the filename ends in `.exe`. It reads
`.env` from the working directory at startup, or uses process environment variables.
Build-time environment values are not embedded. It still needs the OMA backend. Build on the same OS and architecture as the deployment machine.

`just verify` builds the bundled entry and the executable, then runs the test
suite against them. Its process test copies the executable into a temporary
directory, loads a test `.env`, claims a session and completes a CCRv2 turn with
modified approval arguments without source files, `node_modules` or Bun on
`PATH`. The backend, model and sandbox in this test are fixtures.

Without just, use `bun install --frozen-lockfile` followed by
`bun run build:executable` or `bun run verify:executable`. The build adapter makes
`dsh-llm`'s version metadata a static import in memory so Bun can bundle it;
installed dsh files remain unchanged. A changed upstream lookup fails the build
until the adapter is reviewed.

`bin/oma-brain-dsh` remains a Bun launcher for `dist/main.js`, requiring Bun and
`node_modules`. The restart command below uses that module build.

To rebuild and restart the service in the foreground:

```sh
bun run restart
```

The restart script sends SIGTERM to direct Bun entry processes whose working directory is this project, including `bun start` launches. It waits up to thirty seconds for shutdown and cancels the restart if a process remains. It then starts `dist/main.js` in the foreground with output in the current terminal. Press Ctrl+C to stop the service. Run the script as the same user that started the service. A concurrent restart is refused during build and shutdown. If the script is killed and leaves a stale lock, remove `.run/restart.lock` after confirming no restart is running.

The Brain claims Code Sessions with `POST /v1/code/brain/claims?block_ms=25000`, sending the shared fleet token in `OMA_BRAIN_CLAIM_TOKEN` and its process UUID in `X-Worker-ID`. Each claim advances the worker epoch and returns fresh ingress and model credentials to exactly one process. Up to `OMA_BRAIN_CLAIM_CONCURRENCY` long polls run at once, and only while a session slot is free; a busy process stops claiming. Admission registers the worker, loads the session context and restores its journal. If admission fails, the host calls `POST /v1/code/brain/claims/{id}/release`. Transient failures use `reason: "admission_failed"`, so another process can retry after the backend's backoff (5, 10, 20, 40 and 80 seconds); five consecutive failures end the session with `session.error`. Failures that cannot succeed on retry, such as an invalid task envelope, session context or assignment, use `reason: "permanent"`, and the backend publishes `session.error` immediately. Admission is also abandoned with `reason: "shutdown"` when the process is stopping or every local slot is busy, which does not count as a failure. Concurrent admissions reserve a slot before evicting an idle session, so the process never holds more than `OMA_BRAIN_MAX_SESSIONS` sessions. On SIGTERM the host releases claims still being admitted and sessions with an active turn using `reason: "shutdown"`, so another process can take them without waiting for the lease. Idle sessions closed after sixty seconds, evicted for capacity or open at shutdown are released with `reason: "idle"`, which only clears the lease, so new input can be claimed immediately. A crashed process loses its claims when the 20-second lease and 10-second grace period expire.

At startup the host makes a non-blocking claim before reporting ready. If the backend rejects the token, startup fails with `Brain claim rejected with HTTP 401` and the process exits. Other probe failures, such as a backend that is still starting, are retried after 1, 2, 5, 10 and then every 30 seconds. If the token is rejected after startup, the process keeps running its existing sessions, prints the same message once for each run of rejections and keeps retrying with that backoff. The token must match the backend `code_session.brain_claim_token_file`. Startup errors appear directly in the terminal, and `bun run restart` returns the service's exit code.

Each session reads `/worker/events/stream` through the CCRv2 plugin. User turns run serially; control messages use a separate queue so approval and interruption remain responsive during a turn. The bridge acknowledges `envelope.event_id`, not the SSE sequence number. Reconnection keeps the registered epoch and relies on server redelivery without advancing a local cursor. It never registers a new worker or restarts the active turn just because the stream reconnects.

History restoration pages through `GET /worker/internal-events`, omitting `cursor` on the first request and following `next_cursor` until it is absent, null or empty. Foreground events use the default scope. Child `dsh_log` writes include the thread id as the outer event's `agent_id`, and reads use `subagents=true` on every page. Child restoration also reads the foreground stream for logs written by older Brain versions, then merges the scoped suffix using the dsh header id and event sequence. Brain state remains in the foreground stream. These logs use `is_compaction: false`; Brain does not generate Superduck transcript compaction markers.

Capacity defaults to eight sessions. Idle runtimes close after sixty seconds, or earlier when another claim needs capacity. The backend makes a session claimable again when its lease expires and the public session is running. Active turns and approval waits are not evicted. Each session's CCRv2 heartbeat defaults to a six-second interval with ±10% jitter and a five-second request deadline, renewing the 20-second Brain worker lease. Temporary failures keep retrying on that cadence. Authentication failures stop the session at ten failed writes without an intervening successful write. Epoch conflicts, expired worker leases and authentication rejection with an expired token stop immediately. SIGINT and SIGTERM stop consumers, cancel work and in-flight heartbeats, drain state and close sessions. If cleanup takes longer than ten seconds, the process forces exit with code 1. Pressing Ctrl+C again during shutdown forces immediate exit with code 130.

The application reads these optional deployment settings at startup. Unset values use the defaults below; invalid values fail startup before connecting to the backend. Set them in the process environment or `.env` and restart to apply them.

| Variable | Default | Meaning |
| --- | --- | --- |
| `OMA_BRAIN_CLAIM_TOKEN` | required | Shared fleet token matching the backend `code_session.brain_claim_token_file` |
| `OMA_BRAIN_CLAIM_CONCURRENCY` | `4` | Concurrent claim long polls, from 1 through 32 |
| `OMA_BRAIN_MAX_SESSIONS` | `8` | Positive session capacity |
| `OMA_CCR_HEARTBEAT_INTERVAL_MS` | `6000` | Positive integer interval in milliseconds |
| `OMA_CCR_HEARTBEAT_TIMEOUT_MS` | `5000` | Positive integer request deadline in milliseconds |
| `OMA_CCR_HEARTBEAT_JITTER_FRACTION` | `0.1` | Random variation from 0 through 0.5; `0.1` gives ±10% per beat |
| `OMA_CCR_HEARTBEAT_MAX_FAILURES` | `0` | Nonnegative integer limit on transient heartbeat failures; `0` keeps retrying |
| `OMA_CCR_HEARTBEAT_MAX_AUTH_FAILURES` | `10` | Positive integer limit on 401/403 responses across heartbeat and other CCRv2 writes |

Intervals including jitter and request deadlines must fit a 32-bit JavaScript timer. The interval with jitter plus the request deadline must stay below the 20-second worker lease. Existing environment overrides still apply. `409` and `410` stop immediately regardless of either failure limit, as does a heartbeat `404` (`worker_not_registered`), which means the server no longer holds a lease for this worker. For `401`/`403`, an expired JWT also stops immediately; otherwise failures accumulate toward the authentication limit. Successful POST/PUT responses reset that counter, including heartbeats, state updates, publication, persistence and delivery confirmation. GETs and other failed requests do not reset it. Other heartbeat failures, including transport errors, 5xx, 408, 429 and other 4xx, keep scheduling unless a positive transient failure limit is configured.

Set `OMA_DEBUG_SYSTEM_PROMPT=1` to print the system prompt and tool names exactly as sent to the Messages API, as an `oma_system_prompt` JSON line on stdout. Each session and subagent thread prints once, and again only when its prompt or tool list changes. The output contains the configured agent instructions and memory catalog, so enable it only for local debugging. Read it with `jq -r 'select(.event == "oma_system_prompt") | .system'`.

## CCRv2 plugin

The repository-local plugin entry is `src/ccrv2/index.ts`. Install it after the native runtime and supply a backend implementing `CcrBackend`:

```ts
import { mountKernel } from "./src/kernel.ts";
import * as CcrBridge from "./src/ccrv2/index.ts";
import * as OmaSandboxProvider from "./src/oma-sandbox/index.ts";

await mountKernel(ctx);
await ctx.plugin(CcrBridge, { backend });
// Register the model adapter separately through ctx.llm.
const session = ctx.ccrv2.createSession({
  id: sessionId,
  provider: "oma",
  model,
  workDir: "/workspace",
  systemPrompt,
  setup: async (scope) => {
    await scope.plugin(OmaSandboxProvider, { api, sessionId });
  },
});
await session.open();
await session.receive(inputId, payload);
```

The bridge uses dsh services through Cordis dependency injection and exposes `ctx.ccrv2`. It has no imports from the OMA API, assignment scheduler, claim transport, model adapter or MCP implementation. `CcrBackend` supplies confirmed persistence, publication, delivery acknowledgement and worker status operations.

`SandboxExecutor` and `RemoteTool` live in the neutral `src/runtime/contracts.ts` module. `SessionRuntime` owns agent creation and restoration, system prompts, model-request barriers, turn execution and cancellation. It loads the `SandboxTools` plugin inside each agent scope. `ToolExecution` validates original and approved arguments, orders sandbox readiness and dispatch, converts results and defers model context. The runtime has no CCRv2 imports or wire envelopes. Its awaited `ToolExecutionBridge` contract supplies approval and durability operations; its session hooks let the bridge map native events and bracket each turn with confirmed input and terminal commits.

The root session's `setup(scope)` callback mounts host service plugins before tool registration. `SessionRuntime` gives each root a separate `sandboxExecutor` service scope and explicitly joins that scope for child agents. The callback runs only for the root, including when restoring a session. A provider must supply `ctx.sandboxExecutor`; a missing provider fails agent setup. The neutral interface and Cordis service declaration live in `src/runtime/contracts.ts`.

The `OmaSandboxProvider` entry is `src/oma-sandbox/index.ts`. The OMA host loads it through that setup callback with the root session ID and API transport. It owns one `SandboxMcp` instance, shared by the root and its children. Loading the provider performs no network requests. The first MCP handshake happens only after approved arguments have been persisted. The configured server endpoint waits for sandbox readiness during initialization; the Brain sends no resource-creation request. Closing a protocol session drains child runtimes before disposing the root agent and its provider; failed agent setup rolls back the provider.

Model requests use the Vercel AI SDK provider layer only: `@ai-sdk/anthropic` for Anthropic Messages and `@ai-sdk/openai-compatible` for OpenAI Chat Completions, both pinned and called through `doStream`. The protocol translation lives in `src/llm-ai-sdk/`, a DSH plugin (`name`, `inject: ["llm"]`, `apply`) that depends only on `@ai-sdk/*` and `@deepseek-ai/*`: it converts DSH history into the SDK prompt, maps SDK stream parts to DSH chunks, classifies errors and adds DSH attribution headers. OMA loads it with `providers: ["oma"]` and supplies `omaModelOptions` (`src/model.ts`, `src/model-provider.ts`), which selects the backend, binds the root or child session's model token, restricts HTTP requests to the Go proxy, maintains CCRv2 response identity and converts legacy pi-ai replay signatures. The SDK owns SSE parsing, protocol encoding and streamed tool-argument accumulation in linear time. No SDK retries run, ambient provider credentials are unused, and Provider keys remain in Go. Histories keep the `oma-messages-v1` and `oma-chat-completions-v1` replay formats. Messages keeps its 8192 default output cap; Chat sends a cap only when one is requested.

The sandbox provider uses the same official `@modelcontextprotocol/client` SDK as `dsh-mcp-client` for initialization, protocol negotiation, request IDs, session headers, JSON/SSE responses, cancellation and stream cleanup. The complete DSH MCP plugin cannot replace this provider directly: it connects and discovers/registers tools on activation, while this host initializes only after tool approval and exposes its fixed sandbox tools through `ToolExecution`. One SDK connection belongs to each root provider, is shared by its children, and closes when that provider is disposed. `src/sse.ts` parses worker SSE streams; the AI SDK providers and MCP SDK own their respective SSE parsing.

The `SandboxTools` entry is `src/sandbox-tools/index.ts`. It injects the existing `sandboxExecutor` and binds a separate execution bridge for each agent. It registers built-in Bash, Read, Write, Edit, Grep and Glob definitions from `definitions.ts`, with a fresh schema copy for every plugin instance. Each definition delegates execution to `ToolExecution`. The plugin preserves the inherited-tool restriction and serial sandbox dispatch, and its registrations are removed when its fiber or agent is disposed. Archiving a child leaves the shared provider available to the root and other children. Updating the sandbox wire contract requires updating the built-in definitions; startup does not call `tools/list`. `tests/sandbox-contract.test.ts` compares these definitions with a real sandbox server's `tools/list` when `OMA_RUST_MCP_URL` is set.

`ToolExecution` reads a shared permission table keyed by sandbox MCP tool name. Before every primary or child model request, `SessionRuntime` refreshes that table from the current `session_context`. The Go endpoint computes permissions from the latest public Session Agent snapshot, so idle-session policy updates take effect on the next turn. Refresh or validation failure prevents the model request; a missing table clears previous grants and falls back to CCRv2 approval. The sandbox gateway independently checks the latest snapshot for denied tools. Tools whose policy is `allow` skip the `can_use_tool` request and never report `requires_action`; `ask`, `deny` and tools without a policy keep the CCRv2 approval request. Read, Glob and Grep are marked `readOnly`: they skip the dispatch checkpoint, retry a failed sandbox call once, and on a second failure return a failed tool result to the model instead of recording an unknown outcome. `CcrSession` implements that bridge contract. It translates `can_use_tool` responses into a neutral approval decision, records approved arguments, dispatch and results in the unchanged `dsh_brain_state` format, and manages CCRv2 event mapping and outbox publication. It delegates native execution to `SessionRuntime`. The execution pipeline awaits every checkpoint before advancing; moving these responsibilities does not turn persistence into asynchronous event notifications.

`CcrClient` in `src/ccrv2/client.ts` implements the HTTP backend contract. It owns worker registration, SSE parsing and reconnection, worker paths, epoch fields, private event pagination, durable envelopes and their content-derived IDs, retries, ephemeral event flags, delivery confirmation, status updates and heartbeat requests. Its config supplies a generic HTTP transport returning raw `Response` objects, current credentials and session IDs. Tokens are resolved again for every retry; each write retains its original worker epoch. Protocol structures in `src/ccrv2/wire.ts` are copied from the local `superduck-code` implementation using `unknown` for opaque fields.

Worker SSE connections send `Accept: text/event-stream` and retain the highest `id:` sequence from complete frames, parsed with `parseInt` before validating event data, matching Superduck. Reconnects send that sequence as both `from_sequence_num` and `Last-Event-ID`, alongside the original `worker_epoch`. Missing, unparseable or lower sequence IDs do not advance the cursor. The cursor remains in memory for the current worker claim, including across inbound stream replacement. `inboundSequenceNum(id)` exposes it, and `inbound(id, signal, initialSequenceNum)` accepts a cursor when rebuilding a client. A replacement claim starts with a fresh cursor; delivery confirmation continues to use the envelope's `event_id`.

Only `event: client_event` frames with usable payloads reach the worker. Missing or unknown event types, malformed JSON and unusable envelopes are logged and skipped. An explicitly different `session_id` is rejected. Complete frames, including keepalive comments, refresh the 45-second liveness timer. SSE connection failures use a one-second exponential backoff capped at thirty seconds with ±25% jitter, and stop after ten minutes of consecutive connection failures. A successful connection resets that budget. HTTP 401, 403, 404, 409 and 410 stop SSE immediately, because a stale epoch or missing session never recovers on reconnect; other HTTP failures reconnect without using `Retry-After`. Heartbeat and write lease rejection policies still apply independently, and a stream cannot reconnect using credentials for a replacement worker epoch.

Successful writes and heartbeats accept any 2xx response without requiring JSON. Private-event reads still require a valid JSON page; null, omitted and empty cursors end pagination. Delivery batches also use the HTTP success result: `ignored` updates and empty 2xx responses are valid, matching Superduck. Publication, status, persistence and delivery HTTP writes each use a ten-second deadline per attempt, matching Superduck; heartbeat requests retain their five-second default and GETs retain the host's deadline. While a claim's heartbeat binding is active, publication, status, persistence and delivery HTTP writes keep retrying until confirmed, cancelled or rejected by the lease policy. This includes 401/403 within the shared authentication budget and temporary failures beyond three attempts. Other 4xx responses, such as 400, 404, 413 and 422, are permanent request failures: the write is rejected after one attempt without reporting lease loss, so a payload the server cannot accept fails its caller instead of blocking the session's queue. The original caller remains pending throughout retries. Payloads and UUIDs are snapshotted before the first attempt, and a write cannot adopt a replacement epoch. Invalid local JSON remains an error.

Internal-event history reads follow Superduck's per-page policy: at most ten attempts, a thirty-second request deadline, a 500ms exponential base capped at thirty seconds, and up to 500ms jitter. The foreground and subagent readers preserve complete event envelopes and return null when ordinary request failures exhaust retries; the dsh adapter rejects null so failed recovery cannot create an empty session. Empty history is valid. A failed body read is retried with the page; malformed JSON, repeated cursors and `404` (missing code session) are errors without retries. GET worker state also fails immediately on `404` instead of retrying ten times. Callers can cancel history reads, and session startup does. Ownership failures stop recovery immediately, and closing an active claim cancels history requests and retry sleeps.

For publication, status and persistence, clients without an active heartbeat binding and final writes after the binding stops retain at most three attempts for transport failures, 5xx, 408 and 429, rejecting other HTTP failures immediately. This keeps shutdown draining bounded. Write retry delays use the reference uploader's 500ms exponential base, 30s cap and up to 500ms jitter. Valid 429 `Retry-After` hints override the exponential delay within those bounds. Heartbeats remain single-attempt requests; the plugin counts their results separately for each claimed session. Stopping the heartbeat binding cancels active write requests and retry sleeps without reporting unconfirmed writes as successful.

Before publishing a tool approval request, the bridge reports `requires_action` with approval details and `external_metadata.pending_action`. After receiving a decision it returns to `running`, clearing both fields. An idle transition also clears pending action metadata. The accepted-input journal is persisted before reporting `processing`. The bridge reports `processed` only after terminal state, output publication and outbox clearance have been confirmed. Dispatch and result durability barriers retain their existing order.

Delivery receipts use `envelope.event_id` throughout. `CcrClient.inbound()` asynchronously reports `received`, including when used without the bridge. User commands enqueue `processing` after the accepted-input checkpoint without waiting for delivery HTTP confirmation. Controls report `processed` after their effects and output publication, without a redundant `processing` or holding the control queue during delivery retries. User turns still await their final `processed` confirmation after terminal state, output publication and outbox clearance. Redelivered completed inputs flush any pending output and report `processed` again without rerunning the model or tools.

`src/ccrv2/delivery.ts` adapts Superduck's serial uploader to one in-flight request, at most 64 updates per batch and 64 pending updates before backpressure. Its promises resolve on HTTP confirmation; progress and control callers handle them in the background. Failed batches retain their order and contents while retrying with refreshed tokens and the original epoch. Permanent 4xx responses other than 401, 403, 408 and 429 stop the delivery claim like a lease rejection, without reporting lease loss. Empty credentials defer HTTP writes through the same cancellable backoff without consuming or resetting authentication failures. Session shutdown cancels the in-flight request, queued confirmations and backpressure waits. Delivery failures log protocol fields and known error codes, without tokens, event IDs or payloads. Terminal HTTP failures are logged before lease cancellation; caller cancellation and late replies remain silent. Bound sessions use the shared heartbeat authentication budget; standalone clients also stop delivery retries after ten authentication failures. Successful worker writes reset the counter but cannot reopen a stopped delivery claim. A stopped claim stays closed across token refreshes and later delivery calls; a new epoch, successful registration or fresh lease binding starts a new claim.

Long-running user commands repeat `processing` every minute, including while waiting for approval, to refresh the backend's 20-minute per-event ACK mapping. Only one progress receipt is pending at a time. Repetition stops before the final `processed` is enqueued, on a failed turn, or on session shutdown. This timer is separate from worker heartbeats and activity keepalives and does not write checkpoints or enter the durable outbox.

The OMA host connects that client to `OmaApi.requestResponse` and assignment credentials in `src/oma-ccrv2.ts`. `OmaApi` supplies HTTP transport and stores credentials supplied in tasks. `SandboxMcp` implements the sandbox contract. OMA assignment credentials are bound before creating the protocol session.

The bridge's optional `heartbeat` config supplies `bind(id)` and `onLeaseLost(session, failure)`. OMA uses `CcrClient.bindLease(id)` to capture the claimed worker epoch, while tokens are still resolved on every request. If the current assignment has a different epoch, the old check is rejected without sending a request with the replacement credentials. Each session owns its timer, in-flight guard, abort controller and failure counters in `src/ccrv2/heartbeat.ts`. Scheduling starts on session creation, covering recovery before `open()` completes. The defaults are `intervalMs: 20000`, `timeoutMs: 5000`, `jitterFraction: 0.1`, `maxFailures: 0` and `maxAuthFailures: 10`. The scheduler skips overlapping requests for that session and notifies the host once on rejection or exhausted failures. The notification retains the HTTP status when available, including 409. OMA stops that session and closes its runtime and SSE stream.

The bound check's optional `subscribe(listener)` also supplies results from other POST/PUT requests, including direct `heartbeat()` and `checkLease()` calls, to the same authentication counter. The scheduler handles its bound check's returned result without a duplicate subscription notification. A successful write resets the counter; each 401/403 increments it once. This follows Superduck's shared request handling, so ten authentication failures need not mean ten heartbeat intervals. Subscriptions capture both the session and epoch, own the active write cancellation signal, detach when the heartbeat stops, and ignore responses from requests started before a replacement subscription. Cancelled requests do not update the counter. Exhausted authentication, expired credentials, epoch conflicts and expired leases end active write retries. The JWT decoding helpers in `src/ccrv2/jwt.ts` are copied from Superduck. Expiry classification reads the credential actually used by the request, without consulting global credentials or logging the token.

Activity keepalives follow Superduck's opt-in `CLAUDE_CODE_REMOTE_SEND_KEEPALIVES` switch, accepting `1`, `true`, `yes` or `on`, case-insensitively. Sending is disabled by default. The plugin's `keepAlive.enabled` option can override the switch. While model streaming or approved tool work is active, `src/ccrv2/activity.ts` sends `{ type: "keep_alive" }` through `/worker/events` every 30 seconds. Model stream start/end frames and the existing tool approval/result hooks track activity without changing the runtime. Each session aggregates overlapping root and child operations into one timer; approval waits and idle sessions do not send. The activity reason type and environment parser come from Superduck, with activity ownership adapted to OMA's multiple sessions.

Keepalives stay outside the durable outbox and publication chains. They use the ordinary CCRv2 event envelope, ten-second write deadline, retry policy and shared authentication counter, but never delay model/tool progress or advance a checkpoint. At most one keepalive write is pending per session. When activity ends or the session stops, its keepalive request and retry wait are cancelled; closing also drains the pending request. Independent durable writes retain their existing ordering and confirmation rules. Backend implementations must honour the optional cancellation signal on `events()`. Worker lease renewal continues through `/heartbeat` independently.

On lease loss, OMA writes one JSON warning with `event: "ccrv2_lease_lost"`, `session_id`, the request's bound `worker_epoch`, `http_status`, `failure_reason`, `stop_reason` and `consecutive_failures`. Failure reasons distinguish authentication rejection, token expiry, epoch mismatch, lease expiry, other HTTP rejection, rate limiting, server errors, timeout and transport errors. The stop reason distinguishes immediate rejection, an exhausted authentication limit and an exhausted optional transient failure limit. The count corresponds to the failure category that stopped the session. The epoch comes from the original claim even if assignment credentials change before the response arrives. Logs omit credentials, raw transport error messages and response bodies. Cancelling a session does not generate a lease-loss warning.

The optional `heartbeat.onHealth(id, health)` callback reports the first nonterminal heartbeat failure, repeats at most once per minute during the outage, and reports recovery after a successful heartbeat. OMA writes these as `ccrv2_heartbeat_failed` warnings and `ccrv2_heartbeat_recovered` informational logs, with the session, bound epoch, heartbeat failure count and elapsed outage milliseconds. Failed-heartbeat logs also include the HTTP status and classified reason. Successful ordinary writes reset the authentication counter but do not report heartbeat recovery. Cancellation and late responses produce no health event; a failing diagnostic callback does not change request scheduling or lease decisions.

The OMA backend grants Brain claim sessions a 20-second worker lease with a 10-second heartbeat grace period (`internal/codesessions/brain_dispatch.go`); other workers keep 60 seconds. A session becomes claimable by another process only after both expire. `internal/codesessions/session_credentials.go` issues ingress JWTs without an independent `exp`; `session_credential_service.go` checks their worker epoch against the active session, so a superseded credential may be rejected as `401`. The client now follows Superduck's authentication tolerance and continued retries, while retaining OMA's immediate stop on `410` as well as `409`. Retries do not extend the backend lease by themselves, and ten auth failures do not grant two hundred seconds of lease validity. This review does not verify the currently deployed backend revision. Like Superduck, heartbeat scheduling does not consume `Retry-After`.

The claim transport admits opaque task JSON. The host validates the task envelope and claims its assignment before using CCRv2. Worker event envelopes, control classification, deduplication and SSE recovery belong to `src/ccrv2`. Task validation checks launch metadata and the named `mcpServers.sandbox` Streamable HTTP endpoint. The MCP client uses only that endpoint's headers, without adding host credentials or Brain ownership headers.

`session.stop()` stops its heartbeat timer and aborts in-flight heartbeat and active write requests, retry sleeps and runtime work. Late results from a stopped session are ignored. `session.close()` waits for the runtime and heartbeat to settle, disposes the dsh agent and drains queued bridge saves, status changes and publications before returning. Final persistence flushes retain bounded retries. The host calls `ctx.ccrv2.forget(session.id)` to drop bridge and persistence caches, and removes assignment credentials. A new session with the same id starts with its own failure counter. Unloading the bridge cancels and closes its remaining sessions; lease expiration and subsequent claims stay with oma-server. The durable `dsh_log` and `dsh_brain_state` formats and the execution order described below remain unchanged.

## Backend contract

`POST /v1/code/brain/claims` returns this envelope, or `204` when no session is claimable within `block_ms`:

```json
{"session_id":"cse_example","worker_epoch":7,"ingress_token":"<session ingress token>","model_token":"<model credential>","claim_expires_at":"2026-10-18T12:00:30Z"}
```

Admission builds the execution assignment:

```json
{
  "session_id": "cse_example",
  "worker_epoch": 7,
  "ingress_token": "<session ingress token>",
  "model_token": "<model credential>",
  "launch": {
    "version": 2,
    "model": "configured-model",
    "work_dir": "/workspace",
    "system": "Agent instructions",
    "environment_prompt": "Environment instructions",
    "skills": []
  },
  "mcp_config": {
    "mcpServers": {
      "sandbox": {
        "type": "http",
        "url": "https://oma.example/v2/ccr-sessions/cse_example/sandbox/mcp",
        "headers": { "Authorization": "Bearer <session ingress token>" }
      }
    }
  }
}
```

The claim rotates the credential pair and epoch in one transaction, so only the claiming process holds credentials for that epoch. The host calls standard `worker/register` with the ingress token, checks the returned epoch, then fetches `GET /v2/sessions/{id}`. It builds the internal assignment above from that shared context. Same-process registration retries preserve the epoch and credentials. A later claim by another process advances the epoch and fences this one. Deploy the backend and host together.

| Interface | Purpose |
| --- | --- |
| `POST /v1/code/brain/claims?block_ms=N` | Claim a session with the fleet token; returns credentials or `204` |
| `POST /v1/code/brain/claims/{id}/release` | Release a claim with `admission_failed`, `permanent`, `shutdown` or `idle` |
| `POST /v1/code/sessions/{id}/worker/register` | Register the worker and return its epoch |
| `GET /v1/code/sessions/{id}/worker/events/stream?worker_epoch=N` | Receive user and control events |
| `/v1/code/sessions/{id}/worker/*` | Private events, public events, delivery confirmation, status and heartbeat |
| `GET /v2/sessions/{id}` | Shared session context: prompts, model, cwd, pinned skills and subagents |
| `POST /v2/ccr-sessions/{id}/sandbox/mcp` | MCP initialization and sandbox tools |
| `POST /v1/messages` | Messages-compatible model proxy |

Lazy sandbox creation uses the existing Environment Work and Sandbox records. Concurrent initializations reuse the same sandbox creation; there is no Brain-specific sandbox state machine or polling loop.

The host constructs its sandbox endpoint from `OMA_API_URL` and the session ID. The backend authenticates the ordinary CCRv2 credential, prepares or resumes the shared Environment sandbox, and forwards MCP requests without a transaction spanning remote execution. Tool definitions remain embedded in DSH; the Go backend no longer serves a duplicate static catalog. Unknown execution outcomes remain in the DSH journal.

## Approval and execution

The six built-in sandbox tools are registered alongside the primary agent's subagent coordination tools. Child agents receive only sandbox tools. No host filesystem or shell tools are loaded. The original model arguments are validated before requesting CCRv2 `can_use_tool` approval. An allow response can include `updatedInput`; those arguments are validated again before sandbox creation. The MCP connection is initialized once per session after the first approved sandbox provisioning, before persisting dispatch. Text-only and denied turns make no MCP requests.

The dsh `tool/call` event retains the original arguments. The Brain private journal records both the original and approved arguments, followed by the confirmed tool output. Changed arguments are also supplied as model context after the corresponding tool result. No dsh argument-rewrite hook or kernel patch is used. Read, Glob and Grep explicitly declare concurrency safety and can run in parallel within a thread. Bash, Write and Edit remain exclusive within that thread; independent subagent threads can dispatch concurrently. The inspected backend's public `user.tool_confirmation` rejects `updated_input`; parameter replacement is supported at the CCRv2 control-response boundary. Injecting a replacement directly through JetStream completes the Brain turn but leaves the public approval pending. A public approval editor requires backend changes to accept replacement input and settle its approval record.

Public messages and tool results are published before the next tool approval or
model request. The Brain persists their outbox first and waits for text previews
to finish before publishing the corresponding final message. A model step without
text does not create an empty message preview. Previews follow the Claude Managed
Agents event-delta shape: one `event_start` per previewed text or thinking block,
then `event_delta` with `delta.index: 0` for text only (thinking previews are
start-only); there is no `event_stop`. Final `agent.thinking` events are progress
signals without content. Approval argument notices are
published only when the argument values changed; object key order does not count
as a change and does not add model context. The private journal records every
approved call. dsh log flushes with no new events write nothing; only session
creation writes a header-only batch. Within one Brain process, a redelivered
control input that was already handled is acknowledged without repeating its
effect; handled control IDs are not persisted across restarts.

## Subagents

The OMA host enables inline subagents by default. This follows the persistent
subagent pattern in [Claude multiagent orchestration](https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration#delegate-to-subagents),
with one delegation level and at most 25 non-archived child threads per session.
Idle children count toward the limit. Each child has a separate native dsh log
and conversation history, and shares the parent's sandbox, model credential and
approval channel. No additional worker lease is claimed.

The primary agent receives three local tools:

- `list_agents` lists predefined specialists and retained threads.
- `send_to_agent` creates a child and waits for its report. Pass `thread_id` on a
  later call to continue the same child's conversation.
- `archive_agent` archives an idle child, rejects future messages to it and
  frees its place under the limit. Its stored history remains available.

For an inline agent, call `send_to_agent` with `name`, `system_prompt` and
`message`. Inline agents inherit the parent's model and sandbox tools. For a
predefined specialist, use `agent_name` and `message`. A follow-up accepts only
`thread_id` and `message`.

The host's optional `launch.subagents` configuration defines specialists:

```json
{
  "inlineAgents": false,
  "agents": [
    {
      "name": "reviewer",
      "description": "Read-only code reviewer",
      "systemPrompt": "Review the code and report concrete defects.",
      "model": "your-review-model",
      "tools": ["Read", "Grep", "Glob"]
    }
  ]
}
```

These are local definitions supplied in the assignment, rather than Anthropic
agent IDs. There can be at most 20 named definitions. Disabling inline agents
requires at least one definition. An omitted `tools` list inherits all sandbox
tools; an empty list gives a specialist no tools. Tool lists accept only the
six built-in sandbox tool names. The optional model must be available through
the OMA model proxy. Generic CCRv2 callers opt in by supplying `subagents` in
`SessionRuntimeConfig` and bind child model identities through `bindThread` when
their adapter requires it.

`src/runtime/subagents.ts` coordinates child lifecycle, delegation tools and
reports. It depends on the neutral `SubagentBridge` contract in
`src/runtime/contracts.ts`, with no CCRv2 imports or wire envelopes. The injected
`CcrSubagentBridge` in `src/ccrv2/subagents.ts` maps runtime facts to thread events,
binds remote persistence and supplies the existing approval and durability
operations. Thread metadata
is stored in the optional `threads` field of `dsh_brain_state`. Child `dsh_log`
batches are stored under the parent's worker endpoint, distinguished by their
native header ID. Recovery restores child history lazily on follow-up. It never
replays an interrupted delegation. An unknown child sandbox outcome blocks the
whole session for reconciliation, like an unknown primary tool outcome.

Lifecycle and report events use `session.thread_created`,
`session.thread_status_running`, `session.thread_status_idle`,
`session.thread_status_terminated`, `agent.thread_message_sent` and
`agent.thread_message_received`. Child messages, previews, tool results and
approval requests carry `session_thread_id`. User interrupts without a thread ID
cancel the primary and all children. An interrupt naming a child cancels only
that child; the primary receives the failed delegation tool result and can
continue. Closing or losing the parent lease cancels and disposes all children.

Independent delegations run in parallel when the model emits multiple
`send_to_agent` calls in one response. The dsh tool pool allows up to 25
concurrent coordination calls. Each call waits for its child's report, so the
primary continues after the batch settles; this does not detach background
turns. A retained child accepts only one active turn, reserved before any
awaited setup. Read, Glob and Grep can overlap within each thread. Bash, Write
and Edit wait for preceding calls and finish before subsequent calls start.
Shared filesystem writes across threads must be coordinated by the agent.

The journal's optional `executions` map records primary and child execution
states by native session ID. Each thread records `callPhases` for Bash, Write and Edit, approved
original/effective arguments and confirmed outcomes by call ID. Results may
arrive out of order without replacing another call's state. Identical tool call
IDs in separate threads cannot overwrite each other. The legacy top-level phase is an aggregate:
blocked is sticky, and any unresolved dispatch keeps the aggregate in dispatch
regardless of another call or child's completion. On recovery, each unfinished
dispatch becomes blocked and approved but undispatched calls become interrupted;
confirmed results are retained. Legacy top-level tool maps remain for
the primary; old version-1 snapshots without `executions` migrate conservatively
on load. Thread metadata and logs still use the parent's worker endpoint.

Approvals are independently correlated by request ID and thread ID. Worker
status is running while a child is making progress; when all active children
wait on approvals, it is requires_action. Remaining approvals are retained in
`external_metadata.pending_action`, with `pending_actions` when more than one
is outstanding, even if a sibling is running. Cancelling one thread leaves the
others' approvals intact. Durable publication and status writes are serialized
to prevent concurrent outbox removal or stale status overwrites.

Recovery never replays an interrupted turn. Pending approval or pre-dispatch
work, including in-flight read-only calls, becomes interrupted and permits a
fresh input. A dispatched Bash, Write or Edit call without a
confirmed result makes its thread blocked and blocks the entire session for
operator reconciliation. Confirmed sibling outcomes remain recorded. Native
logs for interrupted child threads are repaired on recovery without sampling
the model. Parent shutdown and lease loss cancel and drain child work; active
children also count toward worker capacity and prevent idle eviction.

This repository does not implement Anthropic's REST thread list, event-list,
archive or SSE endpoints. The gateway and UI must retain and route the tagged
worker events to expose separate thread views. The tests
use the real dsh loop with a controlled gateway and model; this change has not
been verified against a production gateway's thread-event validation.

## Persistence and recovery

`CcrPersistence` implements dsh's SessionPersistence interface. Current-format headers and contiguous log batches are stored as CCRv2 private `system` events with subtype `dsh_log`. Input state, completed input IDs, approved arguments, confirmed tool outputs and the public-event outbox use subtype `dsh_brain_state`. The envelopes have schema version 1. Repeated writes retain the same content-derived UUID, allowing the backend to deduplicate a request whose HTTP confirmation was lost. Restoration compares each batch's header and any repeated event sequence by JSON value, ignoring object key order: the backend returns small payloads from PostgreSQL `jsonb`, which reorders keys, and payloads above 32 KiB byte-for-byte from object storage, so batches of one log can come back with different key orders.

Execution order is:

1. Persist the accepted input before confirming delivery.
2. Flush the native dsh log and persist the outbox, then publish preceding messages and tool results before model requests and tool approval.
3. Persist approved arguments, then provision the sandbox.
4. Flush the log and persist `dispatch` before issuing the remote tool call.
5. Persist the confirmed output. Flush and publish the resulting dsh events before the next tool approval or model request.
6. Persist terminal events in the outbox, set the worker idle, publish, then persist the cleared outbox.

Completed turns resume with their dsh model history. An interrupted turn is closed through dsh's existing log-repair mechanism and is not automatically resumed or replayed. Accepted inputs without a recorded dispatch are marked failed and completed, permitting a later user turn. A recorded dispatch with unknown outcome blocks further user input. The session is not repeatedly reclaimed by that Brain process. Operator reconciliation is required; this version has no reconciliation command and must not be unblocked merely to retry a side-effecting command.

Model quota failures (`QUOTA` or `ACCOUNT_QUOTA`) publish an explicit balance/quota message and log `ccrv2_turn_failed` with `model_quota_exhausted`. Public errors and diagnostics do not copy upstream response bodies. Error publication follows the existing persistence and delivery acknowledgement order; a later turn can run after the provider balance or credentials are corrected.

The gateway no longer marks the backend session blocked when an MCP request
fails. CCRv2 checkpoints and heartbeats remain writable, and a replacement Brain
can claim an expired session and restore its journal. The Brain retains its
unknown-outcome recovery policy. Backend regression tests cover forwarding
failures and concurrent requests; the full inflight crash recovery path still
requires real-backend verification.

Grok checkpoints are refused rather than silently converted. This version starts new dsh sessions and restores its own logs. Background jobs and legacy history migration are outside this implementation. On a confirmed CONTEXT_WINDOW_EXCEEDED model error, the runtime summarizes older large, successful text Read results into first/last excerpts with an explicit omission notice; the latest two results, user inputs and assistant/tool-call identities remain unchanged. DSH surface replacements retain the original append-only log and replay the reduced model history. After durable persistence the owning root or child thread emits agent.thread_context_compacted and retries the failed step once. Replacement results do not publish duplicate public tool_result events. Other failures or histories without eligible Read output remain terminal; this is targeted recovery, not general semantic conversation summarization.

## Verify

```sh
bun run typecheck
bun run build
bun test --timeout 15000
bun run verify
```

`verify` builds the application and runs all tests against the bundled entry. The process tests use a fixture backend that implements the claim and CCRv2 endpoints.

The tests exercise the real published dsh loop and the OMA Messages adapter. Model responses, CCRv2 HTTP endpoints and sandbox MCP are controlled. The claim process test launches the application entry in a separate Bun process, returns a claim, verifies exactly one registration request, drives input and approval through SSE, and verifies the idle release at SIGTERM and the handoff of a busy session. Crash testing kills a separate Bun process with SIGKILL during a remote tool call, rotates the epochs, then restores its durable state in a fresh host and checks that the tool is not dispatched again. Other cases cover modified approval arguments, denial, invalid replacement parameters, duplicate input, completed history restoration, persistence rejection, stale worker epochs and terminal outbox recovery after failed publication.

Real-backend and real-model verification uses the existing OMA Go verifier.
See [verification instructions and recorded results](verification/README.md) for
the scenario source, verifier-only patch, reports and public approval limitations.

## Code map

| File                 | Responsibility                                                    |
| -------------------- | ----------------------------------------------------------------- |
| `src/main.ts`               | Bun application host                                     |
| `src/plugin.ts`             | OMA host composition                                     |
| `src/kernel.ts`             | Native dsh runtime composition                           |
| `src/worker.ts`             | Task admission, capacity and worker lifecycle |
| `src/worker-registration.ts` | Standard CCRv2 registration, context loading and task validation |
| `src/ccrv2/index.ts`        | Repository-local CCRv2 plugin entry                       |
| `src/ccrv2/plugin.ts`       | Bridge service, dependency injection and teardown        |
| `src/ccrv2/client.ts`       | CCRv2 HTTP protocol, fencing, retries and confirmation    |
| `src/ccrv2/inbound.ts` | Serial user turns, independent controls and input deduplication |
| `src/ccrv2/heartbeat.ts`    | Per-claim heartbeat scheduling, failure counts and cancellation |
| `src/ccrv2/session.ts`      | CCRv2 mapping, approval messages, durable state and recovery |
| `src/ccrv2/persistence.ts`  | Remote dsh session log backend                           |
| `src/runtime/contracts.ts` | Neutral sandbox, execution and session contracts         |
| `src/runtime/session.ts`   | Native dsh lifecycle, tools, turns and cancellation      |
| `src/runtime/subagents.ts` | Child lifecycle, delegation, follow-up and cancellation |
| `src/ccrv2/subagents.ts` | Subagent protocol events, approvals and persistence adapter |
| `src/runtime/execution.ts` | Tool validation and ordered sandbox execution            |
| `src/runtime/tools.ts`     | Sandbox result text conversion                           |
| `src/sandbox-tools/index.ts` | Agent-scoped sandbox tool plugin and dependency binding |
| `src/sandbox-tools/definitions.ts` | Built-in sandbox names, descriptions and schemas |
| `src/oma-ccrv2.ts`          | HTTP transport and assignment credentials for CcrClient  |
| `src/oma-session.ts`        | OMA assignment and prompt binding                        |
| `src/assignment.ts`         | OMA assignment schema and validation                     |
| `src/json.ts`               | Shared JSON types and object validation                  |
| `src/model.ts`              | OMA model route over the official DSH adapter             |
| `src/oma-sandbox/index.ts`   | Root-session sandbox executor provider plugin           |
| `src/oma-sandbox/client.ts`  | Server-configured MCP initialization and execution               |
| `src/claim.ts`              | Claim long polling, startup probe and release classification |
| `src/capacity.ts`           | Admission slot reservation and idle-session eviction         |

## Messages and sandbox result recovery

Malformed tool argument JSON stays unchanged in the native dsh log. The tool
registry rejects it and records a real failed tool result. When replaying the
assistant call to the Messages API, the adapter represents its non-JSON input as
`{ "__unparsedToolInput": { "raw": "...", "len": 16 } }`. It keeps the original
call ID and sends the recorded result with `is_error: true`, so the model can
continue. The envelope is a serialization boundary representation, never a tool
execution input or a replacement error status. Valid JSON and successful results
retain their existing semantics.

For Bash only, a successful structured execution with completed status, zero
exit code, empty stderr, no cancellation, timeout, truncation or collection error
returns exact stdout. The text must match stdout, and an optional second metadata
text block must parse to the same structured result. Empty stdout may use the
sandbox's `(no output)` placeholder. Model feedback and public CCRv2 tool results
share this conversion and omit duplicate execution metadata. Failures,
inconsistent metadata and other tools retain their diagnostic text.
