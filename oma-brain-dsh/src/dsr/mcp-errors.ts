import { appendPluginRecord, pluginRecordOf, type Session, type SessionEvent } from "@deepseek-ai/dsh-session";
import type { PublicEvent } from "./wire.ts";

type McpErrorType = "mcp_authentication_failed_error" | "mcp_connection_failed_error";
interface McpErrorRecord { version: 1; type: McpErrorType; serverName: string; phase: "discovery" | "tool_call"; context: string; }
declare module "@deepseek-ai/dsh-session/types" {
  interface PluginRecordMap { "plugin:oma/mcp-error": McpErrorRecord; }
}
export function recordMcpError(session: Session, type: McpErrorType, serverName: string, phase: "discovery" | "tool_call"): SessionEvent {
 const events = session.snapshotEvents();
 const boundary = events.findLast(event => event.type === "user/message" || event.type === "tool/call");
 const context = boundary ? String(boundary.seq) : "initial";
 for (const event of events) {
  const record = pluginRecordOf(event);
  if (record?.type === "plugin:oma/mcp-error") {
   const data = record.data as McpErrorRecord;
   if (data.type === type && data.serverName === serverName && data.phase === phase && data.context === context) return event;
  }
 }
 const seq = appendPluginRecord(session, "plugin:oma/mcp-error", { version: 1, type, serverName, phase, context });
 return session.snapshotEvents()[seq]!;
}
export function projectMcpError(event: SessionEvent, sessionID: string, rootID: string): PublicEvent | undefined {
 const record = pluginRecordOf(event);
 if (record?.type !== "plugin:oma/mcp-error") return;
 const data = record.data as McpErrorRecord;
 return { id: `${sessionID}_mcp_error_${event.seq}`, type: "session.error",
  ...(sessionID === rootID ? {} : { session_thread_id: sessionID }),
  error: { type: data.type, mcp_server_name: data.serverName,
   message: data.type === "mcp_authentication_failed_error" ? "MCP server authentication failed" : "MCP server connection failed",
   retry_status: { type: "exhausted" } } };
}
