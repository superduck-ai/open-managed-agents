# Subagent delegation

Run `just verify-be chat doctor subagents`, then `just verify-be chat subagents`. The scenario uses public Agent, Skill, Session and event APIs, the real Runner, Docker/FUSE and Claude Code 2.1.251. The upstream model is scripted. Pair it with `chat roundtrip`, `chat tools` and `chat public` when changing the delegation runtime.

Public Agent requests first reject an unknown child ID, version 0 and a nonexistent version. Create a parent with an unversioned object reference and check that it saves child V1. Upgrade the child to V2; the saved reference must remain V1. Resend the unversioned roster to select V2. Create/update another parent with an explicit V1 reference while V2 exists. After the child reaches V3, both parents must retain their pins; updating only the unversioned parent's name must also retain V2. The final execution parent explicitly pins V1, and its new Session must expand V1. The Worker must delegate to that configured child using its own model, system prompt and uploaded skill content. Thread listing must identify the pinned child. The child thread stream and history must contain the same final event ID.

Child Write permission is `ask` while the parent's default is `allow`. Reject the first Write and approve the second; the file must remain absent until approval and contain the expected bytes afterward. A child-only HTTP MCP server must receive exactly one tool invocation. A later parent turn uses the returned runtime Agent ID with native `SendMessage`; the child model request must retain the previous answer and the public thread ID must stay unchanged.

Required stages are `configured_child_dispatched`, `child_skill_preloaded`, `child_stream_received`, `child_tool_policy_verified`, `child_mcp_executed`, `child_thread_history_verified` and `child_thread_resumed`. A missing stage, skipped test, source mutation or incomplete cleanup fails the scenario.

This covers a single configured child and same-Worker continuation. It does not verify concurrent instances of one configuration, directed interruption, Worker restart recovery, real model reasoning, cloud allocation, UI rendering or filesystem/skill isolation. Child Tunnel gateway selection and OAuth scope preservation have handler tests; this scenario's actual Worker uses a remote HTTP MCP fixture.

See [the delegation design](../../../../docs/design/be/subagent-delegation.md) for configuration, identities and permission boundaries.
