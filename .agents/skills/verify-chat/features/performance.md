# Chat performance

Run `just verify-chat run chat.performance` to measure the current checkout. It runs one session, two warmup turns and twenty measured turns serially. The scripted model returns two text fragments separated by a fixed 200 ms interval, without waiting for the client. All turns must finish correctly, produce one model call each, persist ordered unique history and drain the queue.

Durations are measured from submission start to HTTP acceptance, first observed public preview, final public reply and observed idle/ACK drainage. The last includes up to a polling interval of observation delay. Reports retain every sample and P50/P95. This is a serial chat latency baseline, not a concurrent capacity benchmark or a real model latency benchmark.

```sh
just verify-chat run chat.performance --backend-ref HEAD
just verify-chat run chat.performance --baseline /absolute/path/to/base/report.json
just verify-chat run chat.performance --diagnostics
```

`--backend-ref` exports a local Git commit into the run's private temporary directory and builds that backend; the current checkout supplies the test harness. It never checks out or modifies the working tree. Use the printed report path as the baseline. The gate requires a passing, cleaned, non-diagnostic report with twenty valid samples, matching workload source hash, workload version, host class, tool versions and image IDs. It blocks incompatible comparisons instead of treating them as passes.

The initial policy fails when either P50 or P95 of any metric exceeds `baseline × 1.25 + 25 ms`. This is an explicit initial tolerance, not an empirically established production SLO. The CLI never updates a baseline automatically. On regression, retain the evidence, investigate, and fix or revert the candidate. Baseline updates require review of the measured behavior; changing thresholds to make a regression green is not a fix.

`--diagnostics` enables a separate loopback-only backend listener, waits until warmup finishes, then captures CPU and runtime trace during the measured load. It also saves heap/goroutine profiles and `cpu-top.txt`. It cannot be combined with `--baseline`, and its report cannot become a baseline. These profiles are from the backend process, not the Go test driver. Inspect them with `go tool pprof` or `go tool trace`. They remain local and are not uploaded by CI.

A baseline without a full backend commit SHA is rejected. A passing working-tree measurement is not a baseline: rerun the intended base using `--backend-ref`. Markdown reports include backend/harness provenance, tool versions and image IDs. Diagnostic downloads must be nonempty and at most 64 MiB.
