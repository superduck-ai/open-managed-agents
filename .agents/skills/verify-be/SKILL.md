---
name: verify-be
description: Verify OMA backend Transcript archive, restore, deletion and integrity; Files lifecycle, tenant isolation, invalid uploads and object storage; or chat after changes to Sessions, Worker delivery, tool approval, model proxy, SSE, history, multi-instance delivery, Runner startup or chat performance. Use the Go CLI with isolated dependencies and scenario-specific real services or Workers.
---

# Verify backend

Run from the repository root. The executable is `.agents/skills/verify-be/scripts/verify-be`.
This shell entry point generates Yourbatis sources, builds and executes `cmd/verify-be`. All orchestration and verdict logic is Go; no Python runtime is used. You can also run `./scripts/generate-go.sh`, build that Go command once and invoke the binary from the repository root.
Use this project's `.agents/skills` discovery location. The default suite verifies backend behavior and local sandbox startup. Explicit cloud adapter scenarios verify configured provider operations separately; browser rendering remains outside its scope.

## Agent workflow

Use this workflow when implementing or fixing Transcript, Files or chat paths named in the description. Read the relevant [feature map](features/README.md), then select scenarios by the behavior the change can affect. Pure documentation changes do not require starting the environment.

1. Run `just verify-be transcript doctor`, `just verify-be files doctor` or `just verify-be chat doctor` for the affected domain. If prerequisites are blocked, report the missing requirement; doctor success alone is not application verification.
2. Finish edits and required repository checks before running scenarios. Run generation, builds and checks that regenerate Mapper files serially with verification in this checkout. Keep source unchanged during each run, and avoid competing local loads during performance comparisons.
3. Run `just verify-be DOMAIN SCENARIO` using the selection table below. For Files changes run all nine local functional scenarios plus `files performance` with an explicit base commit and baseline comparison; for shared verification changes also run chat scenarios. Always include `chat.roundtrip` for chat behavior changes; add every relevant scenario when a change crosses boundaries. Cloud scenarios require explicit private configuration and are run separately.
4. For performance-sensitive changes, use the [performance workflow](features/performance.md). Resolve the comparison commit explicitly: use the PR base, or the pre-change commit for local work. `HEAD` is suitable only if it does not already contain the change being evaluated. Measure it with `--backend-ref REF`, then compare the candidate using that run's `report.json` with `--baseline REPORT`. Diagnose regressions in a separate `--diagnostics` run, fix the cause and rerun the comparison. Do not replace a baseline just to make the candidate pass.
5. Read each final report after cleanup. In the delivery response, list the scenarios actually run, pass/fail/blocked results, links to their reports, and uncovered behavior. Include the base commit and baseline report for performance comparisons. A failed run requires investigation; a blocked or skipped run must remain explicitly unverified.

| Changed behavior | Additional scenario |
| --- | --- |
| Transcript archive/export/restore/delete, compaction boundary, pending cleanup or object integrity | All five `transcript` scenarios below |
| Files API, file metadata, workspace authorization, storage or cleanup | `files.lifecycle`, `files.isolation`, `files.invalid`, `files.storage`, `files.attachments`, `files.platform`, `files.recovery`, `files.exhaustion`, `files.generated`, `files.performance` |
| Tool permissions or confirmation | `chat.tools` |
| Busy-input rejection and retry, ACK, Worker reconnection or replacement, history recovery | `chat.reliability` |
| NATS fanout or delivery across backend instances | `chat.instances` |
| Session creation, Runner, mounts or Worker startup | `chat.public` |
| Chat queries, scheduling, streaming latency or other performance-sensitive paths | `chat.performance` with a baseline comparison |

Use the existing Go CLI for orchestration and verdicts. Repository lint and unit-test requirements still apply. When changing a scenario or its contract, update its feature map and the corresponding design documentation.

## Launch

Discover commands and registered scenarios with `just verify-be -h` or `--help`.
Use `just verify-be chat -h` to list scenarios and `just verify-be chat tools -h`
for scenario help. `help`, `help doctor` and `help chat tools` are also supported.
Help exits with code 0 before reading local settings or checking Git/Docker; the shell
entry point still needs Go to build the CLI. Cobra handles commands and shared options.
`--timeout` is shared. `--worker-image` belongs to `chat` and `files generated`; baseline/backend-ref/diagnostics belong to both `chat performance` and `files performance`. Cloud commands and their doctor accept `--cloud-config`. Long options use two dashes,
such as `--worker-image`; `-h` is the short form of `--help`.

Prerequisites: macOS or Linux, repository Go toolchain, Bash, git, tar, Docker Engine with Compose and enough disk space for three JetStream replicas. The CLI generates its ephemeral signing key with Go's standard library. The common images must already exist locally: `postgres:17`, `redis:8`, `nats:2.14.6-alpine`, `pgsty/minio:latest`. Chat additionally needs the Worker image containing `/opt/claude-code/bin/claude`. Only `files generated` needs a Worker image and FUSE; other Files scenarios ignore Worker configuration. The CLI does not pull images. Record image IDs in each report; never compare performance across different images.

```sh
.agents/skills/verify-be/scripts/verify-be chat doctor
.agents/skills/verify-be/scripts/verify-be chat roundtrip
.agents/skills/verify-be/scripts/verify-be chat tools
.agents/skills/verify-be/scripts/verify-be chat reliability
.agents/skills/verify-be/scripts/verify-be chat instances
.agents/skills/verify-be/scripts/verify-be chat public
.agents/skills/verify-be/scripts/verify-be chat performance
```

Worker image selection follows this order: `--worker-image IMAGE`, `OMA_WORKER_CONTROL_IMAGE`, the root `.verify-be.local.json` file's `worker_image` field, then `ghcr.io/superduck-ai/managed-agent-sandbox:latest`. The local settings file is Git-ignored: keep private registry addresses there, out of source and documentation. Once configured locally, no export is needed. Help does not display resolved local values; reports record image IDs. The CLI generates Go sources, builds the actual backend, starts it on `127.0.0.1:18080`, and waits for `/readyz`. It uses a unique Compose project with disposable PostgreSQL, Redis, three NATS nodes and MinIO. It never reads `config/config.yaml` and never kills the listener of an occupied port. Dependency ports are allocated by Docker.

## Doctor

`doctor` / `chat doctor [SCENARIO]` / `files doctor [SCENARIO]` / `transcript doctor [SCENARIO]` checks tools, Docker daemon, image IDs, port 18080 and at least 1 GiB of free Docker volume space. It creates and removes a short-lived probe container and anonymous volume. `chat doctor public` and `files doctor generated` also open the daemon-side FUSE device and verifies mount permissions with the Worker image; macOS alone is not a blocker. Every run performs its own scenario-specific preflight. It does not claim the application works. Scenario execution performs dependency health checks and, when starting a standalone backend, readiness checks before testing. Do not point this runner at an existing API or database. Leave the checkout unchanged while a run is active; source changes invalidate the result.

## Full Go suite

Run `just verify-be test` for all default-build Go packages in disposable PostgreSQL, Redis, NATS and MinIO. This runs `go test ./... -json -count=1` with a default 15-minute package deadline, overridden by `--timeout`. It starts dependencies without a standalone backend, supplies a fresh config/signing key and enables migration, Redis integration, S3 adapter and event-payload S3 integration tests. External test targets and cloud opt-in environment variables are removed from the child environment. Each run removes its containers, volumes and private config.

The suite report lists failures and skipped test names. Required migration/S3/Redis tests must pass. `passed_with_skips` and exit 0 mean the Go command completed with unverified tests explicitly listed; they do not certify skipped tests or end-to-end coverage. Ordinary verification scenarios still reject every skip. Run live Files/chat scenarios separately. Real cloud tests, `e2e` build-tag suites and external Python/TypeScript SDK suites require their own environments and are not covered by this command.

## Transcript scenarios

Read [Transcript coverage](features/transcript.md). Run `just verify-be transcript doctor`, then `integrity`, `recovery`, `concurrency`, `boundary` and `lifecycle` with `just verify-be transcript SCENARIO`.

These reuse existing Go integration fixtures with isolated PostgreSQL schemas and real MinIO. The production retention service and private history HTTP handler run in the test process; lifecycle also builds and invokes the actual maintenance CLI. No standalone backend, Worker or FUSE is required. Upload hooks and schema-local triggers inject deterministic failures; fixtures explicitly age rows and drive cleanup with `RunOnce`. Reports exclude River sweeps, automatic River retries, process crash recovery, cloud IAM and throughput. Lifecycle/recovery default to 5m; the other three default to 3m.

## Files scenarios

Read [Files coverage](features/files.md). Local functional SCENARIO values are `invalid`, `isolation`, `lifecycle`, `storage`, `attachments`, `platform`, `recovery`, `exhaustion` and `generated`. Use `files performance` for a fixed serial workload and [Files performance](features/files-performance.md) for the baseline workflow.
Each uses a disposable backend/database/MinIO with a 64 KiB file limit and 96 KiB workspace quota. A loopback Go proxy forwards S3 requests unchanged; tests can inject bounded, path-scoped 403 responses or connection disconnects and inspect request timestamps. No production fault switch is added.

Uploads intentionally have `downloadable=false`. `lifecycle` uses a separate downloadable fixture; `generated` instead drives a real Worker Write from an SDK-submitted message with preallowed tool permission, actual Runner/FUSE/Filestore and checks public resources, scoped listing and byte equality. Its upstream model is scripted. Run `files doctor generated` to check Worker/FUSE prerequisites.

`recovery` forces deletion and quota-compensation failures, observes automatic background attempts, restores storage and waits for the real one-minute backoff before checking cleanup and retained bytes. No cleanup job is seeded or manually driven in this scenario. `attachments` checks delete conflicts, unlinking, expiry visibility, archive retention and actual Session deletion cleanup. `platform` logs in with the isolated development email flow and tests base64/preview/thumbnail cleanup. `storage` also checks interrupted multipart uploads, invalid credentials and version-specific/all-version deletion in real MinIO. These portable S3 checks do not verify a cloud provider's IAM or network infrastructure.

## Drive

`files exhaustion` runs the real cleanup Worker and real DB/S3 while explicitly expediting a seeded test job before each of ten failures. It checks stored attempts/status, all retry delay values, terminal failure, explicit recovery and completed-job idempotency. It does not wait the cumulative 199-minute schedule; `files recovery` independently checks real one-minute waiting and automatic enqueue.

Cloud adapter commands are `files cloud-storage` and `chat cloud-renewal`. Read [cloud verification](features/cloud.md) before running. They require a private test configuration, never load production/development config implicitly, and report missing prerequisites as blocked. They create bounded run-owned resources, validate real provider responses and reclaim their resources even on assertion failure. No local Docker/Worker is needed. Cloud proof stages are produced directly by the Go CLI's assertions, rather than Go test JSON.

`chat roundtrip` executes `TestChatRoundtrip` with `-count=1 -json` against the newly built backend. Read [the feature map](features/README.md) first. The real Worker calls the real `/v1/messages` proxy; only its upstream response is scripted. The model waits for the public SSE client to observe each Chinese text fragment before continuing. No arbitrary sleep substitutes for that assertion. This deliberately orders preview delivery before the final response; production traffic can persist a final response before the Worker posts its last preview.

Most scenarios prepare Session/CodeSession activation through existing service/DB APIs. The backend's background Runner is disabled to avoid competing for the Worker queue. Agent and Environment creation, message submission, Worker protocol, SSE, model proxy and history use real HTTP. `chat.public` instead creates the Session through the public API and runs the actual Runner with a local Docker Provider, real mounts and environment-manager. See [public startup](features/public-start.md).

`chat tools` executes `TestChatTools`. The existing official `anthropic-sdk-go` dependency sends `user.message`, reads pending tool events and submits `user.tool_confirmation` to the local backend. The scripted upstream requests a Write; the real Worker executes it only after approval. The test runs deny before allow, checks that the file is absent before confirmation, checks the resulting file, and verifies tool-use/result/confirmation/final-message history and drained delivery queues. The SDK does not execute tools itself. See [tool coverage](features/tools.md).

`chat reliability` verifies busy-input rejection without persistence, successful retry after idle, Worker SSE reconnection, mid-stream client interruption and history recovery, then Worker replacement with rotated credentials and rejection of the old credential. `chat instances` routes input/Worker traffic and SSE/history to different backend processes. See [reliability](features/recovery.md).

`chat performance` measures a fixed serial workload. Use `--backend-ref REF` to measure a base commit with the current harness, then `--baseline /absolute/path/report.json` to gate the candidate. `--diagnostics` captures backend profiles in a separate run and cannot be used for a baseline comparison. Read [the performance contract](features/performance.md) before comparing results.

## Evidence

The CLI prints the absolute `tmp/verify-be/<run-id>/` evidence directory. Read `report.json` for the machine verdict and `report.md` for scope and timeline. `tests.jsonl` contains only Go test JSON assertions and stage markers; `tests.stderr.log` keeps separate Go tool diagnostics such as cold-cache module downloads; `server.log`, `build.log`, `dependencies.log` and `cleanup.log` diagnose failures. Evidence is private to the local user. Do not upload raw diagnostic files; share the summary and selected non-sensitive metadata.

Local scenarios require the expected test and package to pass, every proof stage to be present, no skipped tests, an unchanged source fingerprint, and successful cleanup. Cloud scenarios require their direct assertions, all proof stages, unchanged source and successful cleanup. Exit codes: `0` pass, successful doctor or help; `1` failure or performance regression; `2` invalid arguments, incompatible baseline or blocked prerequisites. A skip, no matching test, startup failure or incomplete run is never success. Stage timestamps are observations; `chat performance --baseline REPORT` and `files performance --baseline REPORT` produce performance regression verdicts.

The backend PR workflow has separate Files and Transcript verification jobs without Worker dependencies. `Transcript verification` runs all five archive scenarios. Its `Chat verification` job also runs `files generated` and all chat reliability scenarios, measures the PR base backend and gates the candidate on the same host. It explicitly pulls prerequisite images before invoking this offline-image CLI. Repository branch protection should require `Chat verification`, `Files verification` and `Transcript verification`; adding the workflow does not configure protection. CI uploads only JSON/Markdown verdicts, never raw logs, configurations or profiles.

## Cleanup

Scenario execution cleans up on success, failure, Ctrl-C and SIGTERM. It stops its backend process, removes only Worker containers carrying this run's label, then removes its Compose project and volumes. Config, ephemeral signing key and backend binary are removed; evidence survives.

After an uncatchable SIGKILL or host crash, use the exact run ID and evidence directory printed by the CLI. Inspect the owned resources before removing them:

```sh
docker ps -a --filter label=oma.verify-be.run=RUN_ID
docker compose -p RUN_ID -f /absolute/evidence/directory/compose.json ps -a
```

Remove only the listed Worker container IDs with `docker rm -f`, then use the same Compose arguments with `down --volumes`. Check any remaining backend process against the exact `/absolute/evidence/directory/server` executable before stopping it. Remove that run's `jwt.pem` and `config.json` after cleanup. Never flush shared Redis or purge shared NATS streams. An interrupted report without a final verdict is not proof.

## Helpers

```sh
go test ./cmd/verify-be -count=1
```

These tests cover CLI parsing/help, image configuration precedence and verdict rejection of missing, skipped and incomplete Go tests. They also cover configuration isolation, dependency topology, cancellation, doctor probes and cleanup, diagnostic download bounds and comparable report metadata. Update the feature map and design document when contracts change.

Scenario deadlines default to 3m for roundtrip/tools/instances, 5m for reliability/performance and 6m for public/generated. Files recovery/performance and cloud renewal default to 5m; other Files scenarios default to 3m. Override with `--timeout 8m`; this excludes environment build/startup. Go testing gets 30s cleanup grace and the outer command gets 2m compile/exit grace. Read `failure_kind` to distinguish scenario/outer timeouts, prerequisite blockers and incompatible baselines. A timeout alone is not a measured performance regression.

CLI migration: `verify-chat run chat.roundtrip` is replaced by `verify-be chat roundtrip`. There are no aliases for the old command. Move local Worker settings to `.verify-be.local.json`. Reports and resource labels now use `verify-be`; old evidence remains where it was generated. Root `doctor` checks only common dependencies.
