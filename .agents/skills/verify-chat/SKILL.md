---
name: verify-chat
description: Verify OMA backend chat after changes to Sessions, Worker delivery, tool approval, model proxy, SSE, history, multi-instance delivery, Runner startup or chat performance. Run isolated real Worker scenarios with a Go CLI and official Managed Agents Go SDK.
---

# Verify chat

Run from the repository root. The executable is `.agents/skills/verify-chat/scripts/verify-chat`.
This shell entry point builds and executes `cmd/verify-chat`. All orchestration and verdict logic is Go; no Python runtime is used. You can also build that Go command once and invoke the binary from the repository root.
Use this project's `.agents/skills` discovery location. This skill verifies backend behavior and local sandbox startup; external cloud allocation and browser rendering are outside its scope.

## Agent workflow

Use this workflow when implementing or fixing the chat paths named in the description. Read the relevant [feature map](features/README.md), then select scenarios by the behavior the change can affect. Pure documentation changes do not require starting the environment.

1. Run `just verify-chat doctor`. If prerequisites are blocked, report the missing requirement; doctor success alone is not application verification.
2. Finish edits and required repository checks before running scenarios. Run generation, builds and checks that regenerate Mapper files serially with verification in this checkout. Keep source unchanged during each run, and avoid competing local loads during performance comparisons.
3. Run `just verify-chat run SCENARIO` using the selection table below. Always include `chat.roundtrip` for chat behavior changes; add every relevant scenario when a change crosses boundaries. The PR workflow runs the full set.
4. For performance-sensitive changes, use the [performance workflow](features/performance.md). Resolve the comparison commit explicitly: use the PR base, or the pre-change commit for local work. `HEAD` is suitable only if it does not already contain the change being evaluated. Measure it with `--backend-ref REF`, then compare the candidate using that run's `report.json` with `--baseline REPORT`. Diagnose regressions in a separate `--diagnostics` run, fix the cause and rerun the comparison. Do not replace a baseline just to make the candidate pass.
5. Read each final report after cleanup. In the delivery response, list the scenarios actually run, pass/fail/blocked results, links to their reports, and uncovered behavior. Include the base commit and baseline report for performance comparisons. A failed run requires investigation; a blocked or skipped run must remain explicitly unverified.

| Changed behavior | Additional scenario |
| --- | --- |
| Tool permissions or confirmation | `chat.tools` |
| Busy-input rejection and retry, ACK, Worker reconnection or replacement, history recovery | `chat.reliability` |
| NATS fanout or delivery across backend instances | `chat.instances` |
| Session creation, Runner, mounts or Worker startup | `chat.public` |
| Chat queries, scheduling, streaming latency or other performance-sensitive paths | `chat.performance` with a baseline comparison |

Use the existing Go CLI for orchestration and verdicts. Repository lint and unit-test requirements still apply. When changing a scenario or its contract, update its feature map and the corresponding design documentation.

## Launch

Discover commands and registered scenarios with `just verify-chat -h` or `--help`.
Use `just verify-chat run -h` to list scenarios and `just verify-chat run chat.tools -h`
for scenario help. `help`, `help doctor` and `help run chat.tools` are also supported.
Help exits with code 0 before reading local settings or checking Git/Docker; the shell
entry point still needs Go to build the CLI. Cobra handles commands and shared options.
Options work before or after commands and scenarios. Long options use two dashes,
such as `--worker-image`; `-h` is the short form of `--help`.

Prerequisites: macOS or Linux, repository Go toolchain, Bash, git, tar, Docker Engine with Compose and enough disk space for three JetStream replicas. The CLI generates its ephemeral signing key with Go's standard library. The following images must already exist locally: `postgres:17`, `redis:8`, `nats:2.14.6-alpine`, `pgsty/minio:latest`, and the Worker image containing `/opt/claude-code/bin/claude`. The CLI does not pull images. Record image IDs in each report; never compare performance across different images.

```sh
.agents/skills/verify-chat/scripts/verify-chat doctor
.agents/skills/verify-chat/scripts/verify-chat run chat.roundtrip
.agents/skills/verify-chat/scripts/verify-chat run chat.tools
.agents/skills/verify-chat/scripts/verify-chat run chat.reliability
.agents/skills/verify-chat/scripts/verify-chat run chat.instances
.agents/skills/verify-chat/scripts/verify-chat run chat.public
.agents/skills/verify-chat/scripts/verify-chat run chat.performance
```

Worker image selection follows this order: `--worker-image IMAGE`, `OMA_WORKER_CONTROL_IMAGE`, the root `.verify-chat.local.json` file's `worker_image` field, then `ghcr.io/superduck-ai/managed-agent-sandbox:latest`. The local settings file is Git-ignored: keep private registry addresses there, out of source and documentation. Once configured locally, no export is needed. Help does not display resolved local values; reports record image IDs. The CLI generates Go sources, builds the actual backend, starts it on `127.0.0.1:18080`, and waits for `/readyz`. It uses a unique Compose project with disposable PostgreSQL, Redis, three NATS nodes and MinIO. It never reads `config/config.yaml` and never kills the listener of an occupied port. Dependency ports are allocated by Docker.

## Doctor

`doctor [SCENARIO]` checks tools, Docker daemon, image IDs, port 18080 and at least 1 GiB of free Docker volume space. It creates and removes a short-lived probe container and anonymous volume. `doctor chat.public` also opens the daemon-side FUSE device and verifies mount permissions with the Worker image; macOS alone is not a blocker. Every run performs its own scenario-specific preflight. It does not claim the application works. `run` performs dependency health checks and backend readiness checks before testing. Do not point this runner at an existing API or database. Leave the checkout unchanged while a run is active; source changes invalidate the result.

## Drive

`run chat.roundtrip` executes `TestChatRoundtrip` with `-count=1 -json` against the newly built backend. Read [the feature map](features/README.md) first. The real Worker calls the real `/v1/messages` proxy; only its upstream response is scripted. The model waits for the public SSE client to observe each Chinese text fragment before continuing. No arbitrary sleep substitutes for that assertion. This deliberately orders preview delivery before the final response; production traffic can persist a final response before the Worker posts its last preview.

Most scenarios prepare Session/CodeSession activation through existing service/DB APIs. The backend's background Runner is disabled to avoid competing for the Worker queue. Agent and Environment creation, message submission, Worker protocol, SSE, model proxy and history use real HTTP. `chat.public` instead creates the Session through the public API and runs the actual Runner with a local Docker Provider, real mounts and environment-manager. See [public startup](features/public-start.md).

`run chat.tools` executes `TestChatTools`. The existing official `anthropic-sdk-go` dependency sends `user.message`, reads pending tool events and submits `user.tool_confirmation` to the local backend. The scripted upstream requests a Write; the real Worker executes it only after approval. The test runs deny before allow, checks that the file is absent before confirmation, checks the resulting file, and verifies tool-use/result/confirmation/final-message history and drained delivery queues. The SDK does not execute tools itself. See [tool coverage](features/tools.md).

`run chat.reliability` verifies busy-input rejection without persistence, successful retry after idle, Worker SSE reconnection, mid-stream client interruption and history recovery, then Worker replacement with rotated credentials and rejection of the old credential. `run chat.instances` routes input/Worker traffic and SSE/history to different backend processes. See [reliability](features/recovery.md).

`run chat.performance` measures a fixed serial workload. Use `--backend-ref REF` to measure a base commit with the current harness, then `--baseline /absolute/path/report.json` to gate the candidate. `--diagnostics` captures backend profiles in a separate run and cannot be used for a baseline comparison. Read [the performance contract](features/performance.md) before comparing results.

## Evidence

The CLI prints the absolute `tmp/verify-chat/<run-id>/` evidence directory. Read `report.json` for the machine verdict and `report.md` for scope and timeline. `tests.jsonl` contains only Go test JSON assertions and stage markers; `tests.stderr.log` keeps separate Go tool diagnostics such as cold-cache module downloads; `server.log`, `build.log`, `dependencies.log` and `cleanup.log` diagnose failures. Evidence is private to the local user. Do not upload raw diagnostic files; share the summary and selected non-sensitive metadata.

Passing requires the expected test and package to pass, every proof stage to be present, no skipped tests, an unchanged source fingerprint, and successful cleanup. Exit codes: `0` pass, successful doctor or help; `1` failure or performance regression; `2` invalid arguments, incompatible baseline or blocked prerequisites. A skip, no matching test, startup failure or incomplete run is never success. Stage timestamps are observations; only `chat.performance --baseline REPORT` produces a performance regression verdict.

The `Chat verification` PR workflow runs all reliability scenarios, measures the PR base backend and gates the candidate on the same host. It explicitly pulls prerequisite images before invoking this offline-image CLI. Repository branch protection must require the `Chat verification` check to prevent merging a failing PR. CI uploads only JSON/Markdown verdicts, never raw logs, configurations or profiles.

## Cleanup

`run` cleans up on success, failure, Ctrl-C and SIGTERM. It stops its backend process, removes only Worker containers carrying this run's label, then removes its Compose project and volumes. Config, ephemeral signing key and backend binary are removed; evidence survives.

After an uncatchable SIGKILL or host crash, use the exact run ID and evidence directory printed by the CLI. Inspect the owned resources before removing them:

```sh
docker ps -a --filter label=oma.verify-chat.run=RUN_ID
docker compose -p RUN_ID -f /absolute/evidence/directory/compose.json ps -a
```

Remove only the listed Worker container IDs with `docker rm -f`, then use the same Compose arguments with `down --volumes`. Check any remaining backend process against the exact `/absolute/evidence/directory/server` executable before stopping it. Remove that run's `jwt.pem` and `config.json` after cleanup. Never flush shared Redis or purge shared NATS streams. An interrupted report without a final verdict is not proof.

## Helpers

```sh
go test ./cmd/verify-chat -count=1
```

These tests cover CLI parsing/help, image configuration precedence and verdict rejection of missing, skipped and incomplete Go tests. They also cover configuration isolation, dependency topology, cancellation, doctor probes and cleanup, diagnostic download bounds and comparable report metadata. Update the feature map and design document when contracts change.

Scenario deadlines default to 3m for roundtrip/tools/instances, 5m for reliability/performance and 6m for public. Override with `--timeout 8m`; this excludes environment build/startup. Go testing gets 30s cleanup grace and the outer command gets 2m compile/exit grace. Read `failure_kind` to distinguish scenario/outer timeouts, prerequisite blockers and incompatible baselines. A timeout alone is not a measured performance regression.
