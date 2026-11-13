import { normalizeSandboxResult } from "./output.ts";
import { admitEncodedImages } from "@deepseek-ai/dsh-attachment";
import { withAttachmentSession } from "../attachments/remote.ts";
import type { ContentBlock } from "@deepseek-ai/dsh-llm";
import type { Context } from "@deepseek-ai/cordis";
import { SessionId, SessionSeq, type SessionEvent } from "@deepseek-ai/dsh-session";
import { createToolResultMessage } from "@deepseek-ai/dsh-llm";
import { HttpError } from "../api.ts";
import { CANCELLATION_UNCONFIRMED, type SandboxCalls } from "./client.ts";
import type { SandboxRecovery } from "../runtime/contracts.ts";
import { toolText } from "../runtime/tools.ts";
import type { Payload } from "../json.ts";

const names = new Set(["Bash", "Read", "Write", "Edit", "Grep", "Glob"]);

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

export async function recoverSandboxCalls(ctx: Context, sandbox: SandboxCalls, id: string, signal: AbortSignal, threadId?: string, recovery?: SandboxRecovery): Promise<void> {
  if (!await ctx.get("sessionPersistence")!.stat(SessionId(id))) return;
  const handle = await ctx.get("sessionPersistence")!.open(SessionId(id), "write");
  try {
    const { events } = await handle.read();
    const pending = new Map<string, Extract<SessionEvent, { type: "tool/call" }>>();
    for (const event of events) {
      if (event.type === "tool/call" && names.has(event.data.name)) pending.set(event.data.callId, event);
      if (event.type === "tool/result") pending.delete(event.data.message.toolCallId);
    }
    const settled = await Promise.all([...pending].map(async ([callId, event]) => {
      try {
        const result = normalizeSandboxResult(await settledCall(sandbox, callId, signal, threadId, recovery), event.data.name);
        return { event, result, code: result.errorCode as string | undefined };
      } catch (error) {
        if (!(error instanceof HttpError) || error.status !== 404) throw error;
        const result: Payload = { isError: true, content: [{ type: "text", text: "Tool call was not submitted before the previous owner exited." }] };
        return { event, result, code: "TOOL_NOT_STARTED" as string | undefined };
      }
    }));
    const restored: SessionEvent[] = [];
    for (const { event, result, code } of settled) {
      restored.push({
        type: "tool/result", seq: SessionSeq(events.length + restored.length), time: Date.now(),
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
