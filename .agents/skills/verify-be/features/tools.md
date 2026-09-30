# Confirm or reject a tool call

## Sub-features

Official Managed Agents Go SDK compatibility, public tool confirmation, Worker Write execution, tool result persistence and sandbox file side effects.

## How to get to it (user POV)

Ask a chat to write a file, then deny or approve the pending tool. The SDK sends a `user.message`, reads `session.status_idle` with `stop_reason.type=requires_action`, and sends `user.tool_confirmation` using the pending event ID.

## Driving it with verify-be

Run `just verify-be chat tools`. The deny case must leave the sandbox file absent; the allow case must create the exact expected content. Before either confirmation, the file must be absent. Each history contains exactly one tool use, tool result, confirmation and final answer. The session becomes idle and both delivery queues drain.

## Gotchas

Only the upstream model response is scripted. The SDK talks to the local backend and the actual Worker executes Write. This verifies built-in tool confirmation, not custom tools, MCP, browser approval controls or Runner provisioning. A successful SDK response alone is insufficient evidence; inspect the final CLI verdict including side effects and cleanup.
