import { recoverMcpOAuth } from "../external-mcp/recovery.ts";
import { externalServers } from "../external-mcp/config.ts";
import { sandboxToolDefinitions } from "../sandbox-tools/definitions.ts";
import { cliTool } from "../sandbox-tools/cli.ts";
import { normalizeSandboxResult } from "./output.ts";
import { admitEncodedImages } from "@deepseek-ai/dsh-attachment";
import { withAttachmentSession } from "../attachments/remote.ts";
import type { ContentBlock } from "@deepseek-ai/dsh-llm";
import type { Context } from "@deepseek-ai/cordis";
import { appendPluginRecord, pluginRecordOf, Session, SessionId, SessionSeq, type SessionEvent } from "@deepseek-ai/dsh-session";
import { createToolResultMessage } from "@deepseek-ai/dsh-llm";
import { HttpError } from "../api.ts";
import { CANCELLATION_UNCONFIRMED, type SandboxCalls } from "./client.ts";
import { UnknownSandboxOutcomeError, ToolNotForwardedError, type SandboxRecovery, type ThreadMcpContext } from "../runtime/contracts.ts";
import { toolText } from "../runtime/tools.ts";
import { object, type Payload } from "../json.ts";

const names = new Set(sandboxToolDefinitions().map(tool => tool.name));

async function settledCall(sandbox: SandboxCalls, callId: string, signal: AbortSignal, threadId?: string, recovery?: SandboxRecovery): Promise<Payload> {
  const waiting = recovery ? AbortSignal.any([signal, recovery.interrupt]) : signal;
  try {
    return await sandbox.wait(callId, waiting, threadId, recovery?.blocking);
  } catch (error) {
    if (signal.aborted || !recovery?.interrupt.aborted || error instanceof HttpError) throw error;
  }
  await sandbox.peek(callId, signal, threadId);
  try {
    return await sandbox.cancel(callId, threadId, signal);
  } catch (error) {
    if (signal.aborted) throw error;
    return { isError: true, errorCode: "TOOL_OUTCOME_UNKNOWN", content: [{ type: "text", text: CANCELLATION_UNCONFIRMED }] };
  }
}

export async function recoverSandboxCalls(ctx: Context, sandbox: SandboxCalls, id: string, signal: AbortSignal, threadId?: string, recovery?: SandboxRecovery, mcp?: ThreadMcpContext): Promise<void> {
  if (!await ctx.get("sessionPersistence")!.stat(SessionId(id))) return;
  const handle = await ctx.get("sessionPersistence")!.open(SessionId(id), "write");
  try {
    const { events } = await handle.read();
    const authorizations = new Map<string, Payload>();
    const pending = new Map<string, Extract<SessionEvent, { type: "tool/call" }>>();
    for (const event of events) {
      const record = pluginRecordOf(event);
      if (record?.type === "plugin:oma/tool-oauth") {
        const state = object(record.data);
        authorizations.set(state.callId,state);
      }
      if (event.type === "tool/call") pending.set(event.data.callId, event);
      if (event.type === "tool/result") pending.delete(event.data.message.toolCallId);
    }
    let detached: Session | undefined;
    async function markDispatched(callId: string) {
      detached ??= Session.create(SessionId(id), events, handle.header);
      const seq = appendPluginRecord(detached,"plugin:oma/tool-oauth",{callId,dispatched:true});
      await handle.append([detached.snapshotEvents()[seq]!]);
      await handle.flush();
      await ctx.ccrReplication.flush(id);
    }
    const installed = new Map(sandbox.cliTools.map(cli => [cli.name, cliTool(cli)]));
    const calls = [...pending].filter(([id, event]) => {
      const name = event.data.name;
      if (name.startsWith("mcp__")) return !!mcp && authorizations.has(id);
      return names.has(name) || installed.has(name) || authorizations.has(id);
    });
    const servers = externalServers(mcp?.config);
    const settled = await Promise.all(calls.map(async ([callId, event]) => {
      const state = authorizations.get(callId);
      if (state && event.data.name.startsWith("mcp__") && mcp) {
        const server = servers.find(s => s.name === state.server);
        const result = await recoverMcpOAuth(event.data,state,server,mcp.fetcher,sandbox,signal,markDispatched,threadId,recovery);
        return {event,result,code:result.errorCode as string | undefined};
      }
      try {
        const result = normalizeSandboxResult(await settledCall(sandbox, callId, signal, threadId, recovery), event.data.name);
        return { event, result, code: result.errorCode as string | undefined };
      } catch (error) {
        if (!(error instanceof HttpError) || error.status !== 404) throw error;
        const cli = installed.get(event.data.name);
        if (cli) {
          recovery?.blocking();
          const waiting = recovery ? AbortSignal.any([signal, recovery.interrupt]) : signal;
          try {
            const result = await sandbox.call(cli,
              JSON.parse(event.data.arguments), waiting, threadId, callId, undefined, true);
            return { event, result, code: result.errorCode as string | undefined };
          } catch (cause) {
            signal.throwIfAborted();
            if (cause instanceof UnknownSandboxOutcomeError) {
              return { event, result: { isError: true, content: [{ type: "text", text: CANCELLATION_UNCONFIRMED }] }, code: "TOOL_OUTCOME_UNKNOWN" };
            }
            if (!(cause instanceof ToolNotForwardedError)) throw cause;
          }
        }
        const result: Payload = { isError: true, content: [{ type: "text", text: "Tool call was not submitted before the previous owner exited." }] };
        return { event, result, code: "TOOL_NOT_STARTED" as string | undefined };
      }
    }));
    const restored: SessionEvent[] = [];
    for (const { event, result, code } of settled) {
      restored.push({
        type: "tool/result", seq: SessionSeq((detached?.snapshotEvents().length ?? events.length) + restored.length), time: Date.now(),
        data: { turn: event.data.turn, step: event.data.step,
          message: createToolResultMessage({ callId: event.data.callId, content: await recoveredToolContent(ctx,id,result), isError: !!result.isError }),
          ...(code ? { error: { name: "ToolRecoveryError", code, reason: toolText(result) } } : {}),
        }, surfaceOp: "append", sourceEventSeqs: [event.seq],
      });
    }
    if (restored.length) { await handle.append(restored); await handle.flush(); }
  } finally { await handle.close(); }
}

async function recoveredToolContent(ctx:Context,id:string,result:Payload):Promise<ContentBlock[]> {
 const images=(result.content ?? []).filter((block:Payload)=>block.type==="image");
 if(!images.length)return [{type:"text",text:toolText(result)}];
 const attachments=ctx.get("attachments");
 if(!attachments)throw new Error("Read image attachment storage is unavailable during recovery");
 const refs=await withAttachmentSession(SessionId(id),()=>admitEncodedImages(attachments,images.map((block:Payload)=>({data:block.data,mediaType:block.mimeType}))));
 let index=0;
 return result.content.map((block:Payload):ContentBlock=>block.type==="image"?{type:"image",attachment:refs[index++]!}:{type:"text",text:block.text});
}
