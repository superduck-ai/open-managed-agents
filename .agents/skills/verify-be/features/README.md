# Backend feature map

| User symptom | Map | Runnable coverage |
| --- | --- | --- |
| Rust Brain multi-node ownership, native loop or MCP execution | [Rust Brain](brain.md) | `chat brain` |
| Brain system prompt composition or Skill discovery and loading | [Rust Brain](brain.md) | `chat brain-skills` |
| Attached Memory store metadata or body loading is missing | [Memory](memory.md) | `chat brain-memory` for Brain catalog/approval/MCP; six `memory` scenarios for storage and mounts |
| Bound Skill instructions/references cannot be read or followed | [Skill loading](skills-loading.md) | `chat skills-loading` verifies Brain and Claude; real-model Skill selection remains unverified |
| Sending a message produces no reply | [Roundtrip](roundtrip.md) | `chat roundtrip` |
| Upstream authentication failure or interruption during retry backoff ends incorrectly | [Upstream errors](upstream-errors.md) | `chat upstream-errors` |
| A reply disappears after reopening | [History](history.md) | Successful-turn history in `chat.roundtrip`; mid-stream interruption in `chat.reliability` |
| Tool approval or rejection behaves incorrectly | [Tools](tools.md) | `chat tools` through the official Go SDK |
| Active-input queuing, ordered execution or replacement Worker behaves incorrectly | [Recovery](recovery.md) | `chat reliability` |
| Another API instance cannot stream the reply | [Recovery](recovery.md) | `chat instances` |
| Public Session startup fails | [Public startup](public-start.md) | `chat public` with real Runner and local Docker sandbox |
| Configured child Agent is unavailable, uses parent permissions or loses its thread | [Subagent delegation](subagents.md) | `chat subagents` with real Runner, skills, MCP and same-thread continuation |
| Chat latency regresses | [Performance](performance.md) | `chat performance --baseline REPORT` |

The upstream model is scripted. The local chat scenarios do not verify cloud allocation, automatic cloud fault detection, browser rendering, real model quality or concurrent capacity. The optional cloud adapter scenarios below verify provider operations separately. Each scenario's final report must pass; a successful individual stage is insufficient.

## Files

See [Files lifecycle, isolation, invalid uploads and object storage](files.md). Run `just verify-be files doctor`, nine local functional scenarios and the [Files performance baseline/gate](files-performance.md). `files generated` additionally requires a Worker image and FUSE; run `files doctor generated` for its preflight. [Cloud adapter verification](cloud.md) requires explicit dedicated credentials and is independent of the local suite.

## Transcript

See [Transcript archive, restore and integrity](transcript.md). Run `just verify-be transcript doctor`, then `integrity`, `recovery`, `concurrency`, `boundary` and `lifecycle`. These production-service integration scenarios use real PostgreSQL/MinIO; lifecycle also invokes the maintenance CLI. They do not require a Worker or verify background River scheduling.

## Memory / Filestore

See [Memory / Filestore](memory.md). Run `just verify-be memory doctor`, then `integrity`, `isolation`, `cleanup`, `lifecycle` and `filestore`. These reuse production HTTP handlers over real PostgreSQL/MinIO. Run `memory doctor mounts` and `memory mounts` separately for actual Runner/Docker/FUSE mounting and a fresh sandbox's cross-session reads.

## Deployment / River

See [Deployment / River](deployment.md). Run `just verify-be deployment doctor`, then `lifecycle`, `retry`, `idempotency` and `restart`. These verify production HTTP/Worker paths, real River retries and terminal states, transactional side effects, duplicate occurrences and SIGKILL recovery. Session model turns remain separate; restart uses explicit test-only rescue deadlines.
