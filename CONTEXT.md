# Open Managed Agents

Workspace 里的托管智能体、会话和会话线程。这里的语言描述业务对象本身，不描述存储或 API 形状。

## Language

**Session Agent**:
A Session actually runs with this agent configuration. Identity still names the referenced Agent version; model, system, tools, MCP servers, and skills may differ from that version.
_Avoid_: Agent snapshot as a picture of the Agent resource; thread agent as the session's source of truth

**Override**:
A create-time replacement of selected Session Agent fields. Omitted fields inherit from the referenced Agent version; null or empty clears; a value replaces the field in full. Validation applies only to changed fields and the couplings those changes create.
_Avoid_: merge; patch of the Agent resource

**Session Thread**:
A context-isolated event stream inside a Session, with its own conversation history and status.
_Avoid_: conversation; lane

**Primary Thread**:
The Session Thread that is the session-level event stream; its `parent_thread_id` is null. Its agent configuration is the Session Agent.
_Avoid_: main thread; default thread; a second stored copy as the source of truth
