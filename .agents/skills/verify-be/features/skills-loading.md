# Skill discovery and loading

Run `just verify-be chat doctor skills-loading`, then `just verify-be chat skills-loading`.
Set `OMA_BRAIN_BINARY` to a built Rust executable. The Worker image must contain Claude,
environment-manager, rclone-filestore, `/usr/local/bin/mcp-server` and usable FUSE.

This scenario migrates the focused Skill regression from the abandoned Crush worktree.
Its `prewarm` and `deferred` Host modes depended on that worktree's Go Host runtime.
The current implementation verifies the supported `brain` and `sandbox` runtimes instead,
using the same public Skill upload and Agent binding contracts. Required proofs are
`skill_loading_brain` and `skill_loading_sandbox`; either missing proof rejects the run.

Each mode first denies a Skill Read and checks matching tool, result and confirmation IDs.
Denied content must stay out of model requests and history. A text turn then checks catalog
metadata without eagerly inserting Skill body or reference text into the system prompt.
Approved Reads load SKILL.md and a reference through the runtime's real file tool.
An ordinary Read checks sandbox reuse. The actual readonly FUSE mount rejects mutation,
and the original Skill bytes must remain intact. All turns check public SSE, final output,
idle state and a drained inbound queue.

Brain starts a real Rust process and uses sandbox MCP. Denial and catalog discovery create
zero sandboxes; the first approved Read creates exactly one. Claude starts in one sandbox
through the real Environment Manager and Worker, then reuses it for all Reads.
There is no backend Host filesystem execution path in this scenario.

The model is scripted. The scenario verifies loading and permission boundaries, rather
than a real model's autonomous Skill selection or obedience. `chat brain-skills` separately
checks latest-version pinning, new-version loading, prompt composition and cleanup.
See [the verification design](../../../../docs/design/be/skill-verification.md) for implemented
coverage and proposed extensions. Source changes and cleanup errors reject either scenario.
