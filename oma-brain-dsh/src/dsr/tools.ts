import type { ToolIdentity } from "./wire.ts";

const builtins: Record<string, string> = { Bash: "bash", Read: "read", Write: "write", Edit: "edit", MultiEdit: "edit", Glob: "glob", Grep: "grep", WebSearch: "web_search", WebFetch: "web_fetch" };

export function dsrToolIdentity(name: string): ToolIdentity {
  const match = /^mcp__(.+?)__(.+)$/.exec(name);
  if (match) return { kind: "mcp", server: match[1]!, name: match[2]! };
  if (["list_agents", "subagent", "send_message", "interrupt_agent"].includes(name)) return { kind: "delegation", name };
  if (name === "AskUserQuestion") return { kind: "custom", name: "ask_user_question" };
  return { kind: "builtin", name: builtins[name] ?? name };
}
