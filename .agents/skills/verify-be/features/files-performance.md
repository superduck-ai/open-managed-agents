# Files performance

Run an explicit base backend with the current harness, then compare the candidate on the same host with identical image IDs and tools:

```sh
just verify-be files performance --backend-ref BASE_COMMIT
just verify-be files performance --baseline /absolute/base/run/report.json
just verify-be files performance --diagnostics
```

The fixed workload performs two warmups and twenty measured rounds, serially, with 32 KiB deterministic binary content and a 250 ms gap between rounds. Each round measures upload, metadata GET, list GET, downloadable-fixture GET and DELETE separately. Durations cover client request completion including body reads; upload includes multipart encoding and list includes response decoding. DB fixture creation, object byte assertions and post-delete checks are outside the measured operations. Every round verifies identity/content and deletes its data. Final assertions require empty metadata, objects and quota usage.

Uploads are not downloadable under the existing API contract. Download measurements use a separate real S3/DB downloadable fixture. Worker generation and FUSE are covered by `files generated`, not included in these latency measurements. This is a serial 32 KiB API latency workload, not a capacity or large-file throughput benchmark.

Reports retain all twenty samples and P50/P95 for each operation. Baselines must pass, finish cleanup, contain an explicit full backend commit, and match scenario, workload version/source hash, host, image IDs and tools. A chat baseline cannot serve as a Files baseline. Working-tree measurements and diagnostic runs cannot serve as baselines.

The initial gate rejects P50 or P95 above `baseline × 1.25 + 25 ms`, matching the existing chat policy. It is an initial regression tolerance, not a production SLO. Keep a failing baseline comparison unchanged; diagnose and fix or revert the candidate. Never regenerate a baseline to hide failure. Diagnostics capture backend CPU/heap/goroutines/trace and cannot be compared as a normal performance run.
