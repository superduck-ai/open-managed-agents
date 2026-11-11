import { Session, SessionId } from "@deepseek-ai/dsh-session";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { expect, test } from "bun:test";
import { recordMcpError, projectMcpError } from "../src/dsr/mcp-errors.ts";
test("MCP errors retain child ownership and distinguish a later attempt", () => {
 const session = Session.create(SessionId("child"));
 const first = recordMcpError(session, "mcp_connection_failed_error", "peer", "discovery");
 expect(projectMcpError(first, "child", "root")).toMatchObject({ session_thread_id:"child", error:{mcp_server_name:"peer"} });
 expect(recordMcpError(session, "mcp_connection_failed_error", "peer", "discovery").seq).toBe(first.seq);
 session.append("user/message", createUserMessage({source:{kind:"user"},content:[{type:"text",text:"retry"}]}), {surfaceOp:"append"});
 expect(recordMcpError(session, "mcp_connection_failed_error", "peer", "discovery").seq).not.toBe(first.seq);
});
