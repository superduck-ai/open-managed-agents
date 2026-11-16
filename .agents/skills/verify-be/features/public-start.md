# Public session startup

Run `just verify-be chat public`.

Create a Session through the public API, submit its first message, and let the actual environment Runner claim the queued work. A test-only Provider allocates a Docker container instead of calling the E2B cloud API. The real Runner writes the filestore configuration, starts the image's rclone mounts, waits for readiness, creates the Code Session, sends credentials to the real environment-manager over stdin and publishes runtime metadata. The manager starts the real Worker. Public SSE, final history and queue drainage must then pass.

The test omits the Session title and checks that final history contains only the normal answer and one model request per chat turn. This also detects background title responses leaking into public `agent.message` history.

The Docker container requires `/dev/fuse`, `SYS_ADMIN` and an unconfined AppArmor profile for the real mounts. It is labeled with the verification run ID and removed after testing. The preinstalled image must contain environment-manager, rclone-filestore and Claude.

The backend's E2B connect/timeout/delete calls use a local control API that checks the run-scoped container name and ownership label. Positive timeout requests leave the local container running; deletion removes it. Docker has no cloud TTL, so this does not verify cloud renewal.

This covers the public application startup path and real sandbox commands. It does not certify the external cloud provider's API, network restrictions, billing or allocation capacity. No startup or mount stage is replaced with an unconditional success.

Run `just verify-be chat doctor public` first. This checks the Docker daemon's FUSE device and mount capabilities, including when the CLI runs on macOS. The probe is removed afterward.

To verify an independently built Linux Manager, set `OMA_MANAGER_ARTIFACT_PATH` to the host binary and `OMA_MANAGER_ARTIFACT_MODE` to `path` or `url`, then run `just verify-be chat public`. Run both modes to cover local-file loading and HTTP download. The URL mode serves the binary from a temporary local HTTP endpoint. The fixture first configures an incorrect checksum and verifies that the work stops and its allocated sandbox is removed. It then uses the correct checksum for the normal public startup assertions. The configured image Manager path is deliberately nonexistent, so successful turns require the uploaded artifact. These settings also apply to other scenarios that use `startPublicRunner`; checksum rejection is checked by `chat public`.

The production Runner.Start loop runs in the test process with two workers, before any Session API request. One allocation fails deliberately and its work must stop; the same running loop then processes two new Sessions and completes two real chat turns. This covers continued polling after errors and multiple jobs, without asserting exact tick timing. The backend process's cloud Runner remains disabled. The test cancels and joins the loop before fixture cleanup.

Set `OMA_MANAGER_ARTIFACT_VERSION` to the built binary's exact version, greater than `0.1.0`, to additionally run the Manager version matrix within `chat public`. Use the ARM64 or AMD64 artifact matching the selected Worker image. The current fixture image must contain a Manager older than that version. The matrix exercises older-version upload and symlink switching, equal-version reuse, newer-version reuse against a `0.1.0` requirement, and reuse of a previously installed SHA artifact followed by symlink switching. Existing-version fixtures use the actual supplied binary, not a fabricated `--version` response. In local-path mode, equal/newer cases deliberately configure an unreadable source path to prove that source loading is skipped. Both path and URL modes assert upload counts, the `/usr/local/bin/environment-manager` symlink target, selected binary checksum/version, preservation of the image binary and absence of a partial artifact; every case completes a real Worker chat turn. This remains local Docker sandbox verification and does not exercise cloud pause/resume or upgrading a running process.
