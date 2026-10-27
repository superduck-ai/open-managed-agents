# Live verification

Use the registered backend scenarios from the repository root; no Go template
or harness patch needs to be copied into `tests/liveworker`.

Build the standalone Brain executable with `just build` in `oma-brain-dsh`,
then point `OMA_BRAIN_BINARY` at the resulting `dist/oma-brain-dsh`:

```sh
OMA_BRAIN_BINARY=/absolute/oma-brain-dsh/dist/oma-brain-dsh \
.agents/skills/verify-be/scripts/verify-be chat doctor brain

OMA_BRAIN_BINARY=/absolute/oma-brain-dsh/dist/oma-brain-dsh \
.agents/skills/verify-be/scripts/verify-be chat brain
```

Read the repository's [verification workflow](../../.agents/skills/verify-be/SKILL.md)
for prerequisites, isolated resources, scenario selection and coverage limits.
The registered Brain scenarios use a scripted upstream model; the historical
real-model `chat brain-dsh-live` scenario below is not a current command.

The retired [Go template](https://github.com/superduck-ai/open-managed-agents/blob/9a2dc5dacd0351121eef23ddc23b7533651d5366/oma-brain-dsh/verification/testdata/chat_brain_dsh_test.go)
and [harness patch](https://github.com/superduck-ai/open-managed-agents/blob/9a2dc5dacd0351121eef23ddc23b7533651d5366/oma-brain-dsh/verification/backend-harness.patch)
remain available in Git history. They depend on removed Brain lease and secret
APIs and cannot compile against the current backend. The reports and runtime
fingerprints below are historical evidence, not acceptance of the current build.

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
source and dependency lockfile. It is a snapshot of that run and intentionally
does not track later source changes; for example, it still lists `src/transport.ts`.

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
