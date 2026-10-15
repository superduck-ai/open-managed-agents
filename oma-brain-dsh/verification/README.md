# Live verification

`testdata/chat_brain_dsh_test.go` is a historical source template that runs inside the existing OMA Go `verify-be` harness.
It needs a backend revision with Brain gateway support. It uses a real backend,
NATS cluster, database, MCP server and model.

Build this project with `just build`, then set `OMA_BRAIN_BINARY` to the
absolute path of `dist/oma-brain-dsh`. This executable includes Bun and its
dependencies. The build adapter bundles dsh's dynamic version metadata.
The recorded results below predate this packaging change and used the Bun launcher.

Copy the Go template from `testdata/` into a separate backend worktree at
`tests/liveworker/chat_brain_dsh_test.go` and run `git apply --unidiff-zero /path/to/backend-harness.patch` there.
The patch registers `chat brain-dsh-live` in the existing verifier and lets its
standard Brain history assertion recognize native dsh logs. It does not change
backend HTTP handlers or dsh packages. Keep source stable while tests run.

Create a private mode-0600 JSON file with `base`, `model` and `key`, then set
`OMA_DSH_LIVE_CONFIG` to its absolute path. It identifies the explicitly selected
real OMA Messages endpoint and workspace API key. Never commit that file.

```sh
OMA_BRAIN_BINARY=/absolute/oma-brain-dsh/dist/oma-brain-dsh \
OMA_DSH_LIVE_CONFIG=/absolute/private-live-model.json \
.agents/skills/verify-be/scripts/verify-be chat doctor brain-dsh-live \
  --worker-image oma/managed-agent-sandbox:latest

OMA_BRAIN_BINARY=/absolute/oma-brain-dsh/dist/oma-brain-dsh \
OMA_DSH_LIVE_CONFIG=/absolute/private-live-model.json \
.agents/skills/verify-be/scripts/verify-be chat brain-dsh-live \
  --worker-image oma/managed-agent-sandbox:latest
```

The verifier provisions isolated PostgreSQL, Redis, three NATS nodes and MinIO,
starts the actual Go backend and Docker MCP server, and launches Bun Brain
processes. Its stages check:

- A real model requests Write, public approval authorizes execution, and the
  sandbox contains the exact requested bytes.
- After SIGKILL and natural lease expiration, a replacement Brain restores
  history, the model recalls earlier content, and the old epoch is denied.
- The public API rejects `updated_input` with HTTP 400. A CCRv2 control response
  with changed arguments is sent through the real JetStream reply lane.
  Only the approved path and content are written; both argument versions persist.
  The private turn completes and deliveries drain, while the public approval
  remains pending. The test asserts that limitation, then terminates the session.
- SIGKILL during a real Bash call leaves a durable dispatch record. Takeover
  marks the session blocked, emits a recovery error and does not repeat the effect.

Parameter replacement is verified at the Brain's CCRv2 input boundary. A public
approval editor still requires backend support. The Brain gateway is inherited
from the Rust service. CCRv2 SSE can replace Brain NATS consumption and the private
`delivery` route. Initial assignment and lazy sandbox creation need separate
scheduling/resource APIs.

Read the final `report.json`, `report.md` and `dsh-live-evidence.json` after cleanup.
Raw logs and private configurations are not copied into this project.
Cancelled and failed attempts are not passes.

## Recorded results, 2026-10-13

The backend worktree was isolated from the local `codex/oma-brain-lazy-sandbox`
checkout at `93c525bdfc40c27639bea5918f3331554669031d`, including its existing
uncommitted changes. Each report records its exact verification source hash.
The original backend checkout and Rust Brain project were not modified.
Bun was `1.4.2`; dsh packages were `0.2.1-alpha.2`. The live model was
`deepseek-flash` through `http://127.0.0.1:38080` using the supplied workspace key.
The temporary credential file was deleted after verification.

| Scenario              | Final result                                                                        | Evidence                                                                                                                                                                                |
| --------------------- | ----------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `chat.brain`          | PASS, all nine required stages and cleanup                                          | [report.md](reports/verify-be-5a11999ea65a/report.md), [report.json](reports/verify-be-5a11999ea65a/report.json)                                                                        |
| `chat.brain-dsh-live` | FAIL, cancelled after diagnosing the inflight takeover blocker; three stages passed | [report.md](reports/verify-be-d2f59a1f152f/report.md), [report.json](reports/verify-be-d2f59a1f152f/report.json), [observations](reports/verify-be-d2f59a1f152f/dsh-live-evidence.json) |

The standard scenario used a scripted upstream with the real Go backend,
three-node NATS cluster and Docker MCP/FUSE. It predates the signed-thinking
adapter addition. Its inherited report text says Rust; `OMA_BRAIN_BINARY` actually
pointed to this project's Bun launcher. The live run used the current runtime
recorded in [runtime-fingerprints.json](runtime-fingerprints.json). The verifier
hashes the launcher; this additional manifest also hashes the bundled module,
source and dependency lockfile.

The real model passed public allow approval and exact file writes, completed-turn
history restoration after SIGKILL and natural lease expiry, epoch fencing, and
replacement Write arguments delivered through CCRv2. Public argument replacement
is still incomplete: the API returns HTTP 400, and direct wire injection leaves
the public approval pending even though the Brain turn completes.

Inflight recovery failed at the existing backend gateway. After Bash wrote one
line and the Bun process was killed, the gateway set `brain.blocked=true`. The
lease expired, but `BrainMapper.Available` excludes blocked sessions. No replacement
Brain could restore the journal; its last private phase remained `dispatch`, and
the public session remained running. The file had one line and five bytes, so no
repeated side effect was observed. This is not a successful recovery verdict.
The test still requires replacement recovery and therefore remains a failing
acceptance scenario until the scheduling/recovery contract is changed.

Earlier failed runs are retained for diagnosis:

| Run                                                      | Reason the run was stopped                                                                              |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| [6f66c89552b9](reports/verify-be-6f66c89552b9/report.md) | Standalone Bun binary could not resolve dependency package metadata; replaced with module launcher      |
| [5089046bb3e5](reports/verify-be-5089046bb3e5/report.md) | Real backend exposed null discovery and initialization-control handling gaps; both fixed                |
| [893942b86ca0](reports/verify-be-893942b86ca0/report.md) | Cancelled during startup to finish verifier quality checks                                              |
| [754aeff52b21](reports/verify-be-754aeff52b21/report.md) | Test treated the model's initial Read approval as Write; fixture fixed                                  |
| [21eb0eb4b28b](reports/verify-be-21eb0eb4b28b/report.md) | Modified Write succeeded, but the public approval remained pending; limitation now asserted separately  |
| [16e4ea5d607c](reports/verify-be-16e4ea5d607c/report.md) | Fixture compared public `sevt_` IDs with CCR delivery `csev_` IDs; assertions now check each separately |

All recorded runs completed resource cleanup. The verifier's port 18080 is free;
the shared backend and its services were left running.

`bun run typecheck` and `bun run verify` passed, with 11 tests and 532 assertions.
The verifier package tests and liveworker compilation passed. Backend duplicate
and complexity checks passed. `just lint` and `just dead-code` could not complete
because the installed analyzer could not read the Go 1.27 export format; these
checks are not reported as passing.

Direct CCRv2 SSE consumption, public approval editing, complete inflight recovery,
cloud allocation, network partitions, legacy history migration and production
performance remain unverified. No dsh kernel or backend production handler was
changed by the verification patch.

## Event ordering correction, 2026-10-13

This correction was made after the recorded real-backend runs above. Their
runtime fingerprints remain historical; they do not describe this later build.

The previous adapter retained intermediate messages and tool results until the
whole turn ended. Regression tests reproduced the resulting order: later tool
approvals and model requests preceded earlier tool results. Tool-only model
steps also emitted empty message previews.

The adapter now persists and publishes pending results before the next tool or
model request, waits for previews before final messages, and opens a message
preview only when text arrives. Unchanged approval arguments remain in the
private journal without an extra public system message.

`bun run typecheck` and `bun run verify` passed with 14 tests and 734 assertions.
Coverage includes multiple serial tools in one model response, multiple model
steps, matching preview/final message IDs, rejection of empty previews, and a
failed tool-result publication followed by restart without resampling or tool
re-execution. The verification uses the original dsh loop and an isolated real
NATS server with controlled model, backend HTTP and MCP responses. The Go
real-backend scenarios and the user's live browser session were not rerun for
this correction.
