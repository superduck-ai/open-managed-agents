# Backend feature map

| User symptom | Map | Runnable coverage |
| --- | --- | --- |
| Sending a message produces no reply | [Roundtrip](roundtrip.md) | `chat roundtrip` |
| A reply disappears after reopening | [History](history.md) | Successful-turn history in `chat.roundtrip`; mid-stream interruption in `chat.reliability` |
| Tool approval or rejection behaves incorrectly | [Tools](tools.md) | `chat tools` through the official Go SDK |
| Busy rejection, idle retry or replacement Worker behaves incorrectly | [Recovery](recovery.md) | `chat reliability` |
| Another API instance cannot stream the reply | [Recovery](recovery.md) | `chat instances` |
| Public Session startup fails | [Public startup](public-start.md) | `chat public` with real Runner and local Docker sandbox |
| Chat latency regresses | [Performance](performance.md) | `chat performance --baseline REPORT` |

The upstream model is scripted. The local chat scenarios do not verify cloud allocation, automatic cloud fault detection, browser rendering, real model quality or concurrent capacity. The optional cloud adapter scenarios below verify provider operations separately. Each scenario's final report must pass; a successful individual stage is insufficient.

## Files

See [Files lifecycle, isolation, invalid uploads and object storage](files.md). Run `just verify-be files doctor`, nine local functional scenarios and the [Files performance baseline/gate](files-performance.md). `files generated` additionally requires a Worker image and FUSE; run `files doctor generated` for its preflight. [Cloud adapter verification](cloud.md) requires explicit dedicated credentials and is independent of the local suite.

## Transcript

See [Transcript archive, restore and integrity](transcript.md). Run `just verify-be transcript doctor`, then `integrity`, `recovery`, `concurrency`, `boundary` and `lifecycle`. These production-service integration scenarios use real PostgreSQL/MinIO; lifecycle also invokes the maintenance CLI. They do not require a Worker or verify background River scheduling.

## Memory / Filestore

See [Memory / Filestore](memory.md). Run `just verify-be memory doctor`, then `integrity`, `isolation`, `cleanup`, `lifecycle` and `filestore`. These reuse production HTTP handlers over real PostgreSQL/MinIO. Run `memory doctor mounts` and `memory mounts` separately for actual Runner/Docker/FUSE mounting and a fresh sandbox's cross-session reads.
