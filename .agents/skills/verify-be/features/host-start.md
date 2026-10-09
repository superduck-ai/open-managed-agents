# Host Agent startup

Run `just verify-be chat doctor host`, then `just verify-be chat host`.
Use a local sandbox image containing `/usr/local/bin/mcp-server`, with qoder's
`-transport=http`, `-addr` and `-workdir` flags. The image also needs the existing
rclone-filestore and FUSE runtime. Set private image addresses through the CLI
flag or ignored local configuration.

The test creates and updates an Agent through HTTP with
`metadata.agent_runtime_mode=host`, creates Sessions through `/v1/sessions`,
and submits user messages and tool confirmations through the public API.
The production Runner and `agentruntime.Service` run in the Go test process;
they use the real database-backed HostWorker, Redis and JetStream. The separate
backend serves public HTTP, model proxy and SSE. Core NATS delivers host events
to its subscribers.

Runner provisions actual Filestore mounts in a labeled Docker sandbox and uses
`sandbox_mcp_command` to start the real MCP executable. The Docker Provider
resolves the randomly published MCP port. No model, Worker, history store or
approval adapter is injected into the Agent loop. The host bridge invokes
Crush's original `SessionAgent.Run`; Fantasy is an unmodified module dependency.
Only the upstream model's
responses are scripted.

The scenario rejects a Write before allowing a Write in another Session. Both
cases check absence before confirmation, matching tool IDs and results in public
history, streamed/final text identity and drained queues. A second input checks
continued private history. Process inspection requires the MCP service and
rejects Bun and Claude Agent processes in the sandbox.

Cleanup stops and joins Runner and host Service before removing only run-owned
sandboxes and dependencies. This is public startup coverage with a local Docker
Provider. It does not verify cloud E2B allocation, qoder MCP bearer enforcement,
real model quality, crash recovery, memory usage or production capacity.
