import { isDeepStrictEqual } from "node:util";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { validateJsonSchemaValue, type JsonSchemaNode } from "@deepseek-ai/dsh-tools";
import { object, type Payload } from "../json.ts";
import { toolText } from "./tools.ts";
import {
  ToolNotForwardedError,
  type ApprovalDecision,
  type RemoteTool,
  type SandboxExecutor,
  type ToolExecutionBridge,
  type ToolExecutionContext,
  type ToolPermissions,
} from "./contracts.ts";

/** Ordered tool execution. The bridge supplies policy and confirmed journal writes. */
export class ToolExecution {
  constructor(
    readonly sandbox: SandboxExecutor,
    readonly bridge: ToolExecutionBridge,
    readonly permissions?: ToolPermissions,
  ) {}

  async execute(tool: RemoteTool, original: Payload, exec: ToolExecutionContext): Promise<string> {
    if (this.bridge.blocked())
      throw new Error("Session requires tool outcome reconciliation");
    const validate = (args: Payload) => {
      const errors = validateJsonSchemaValue(tool.inputSchema as JsonSchemaNode, args);
      if (errors.length)
        throw new Error(`Invalid ${tool.name} arguments: ${errors.join("; ")}`);
    };
    validate(original);
    const decision: ApprovalDecision = this.permissions?.[tool.remoteName] === "allow"
      ? { allowed: true }
      : await this.bridge.approve({ callId: exec.callId, toolName: tool.name, input: original }, exec.signal);
    if (!decision.allowed)
      throw new Error("Tool use denied by OMA");
    const effective = decision.input === undefined ? original : object(decision.input);
    validate(effective);
    exec.signal.throwIfAborted();
    const readOnly = !!tool.readOnly;
    const call = { callId: exec.callId, name: tool.name, original, effective, readOnly };
    await this.bridge.recordApproved(call);
    await this.sandbox.provision?.(exec.signal);
    exec.signal.throwIfAborted();
    if (!readOnly) {
      await this.bridge.flushLog();
      await this.bridge.recordDispatch(exec.callId);
    }
    let result: Payload;
    try {
      result = readOnly ? await this.callWithRetry(tool, effective, exec.signal) : await this.sandbox.call(tool, effective, exec.signal);
    } catch (error) {
      if (readOnly || error instanceof ToolNotForwardedError) {
        const failure = error instanceof ToolNotForwardedError ? error.message
          : exec.signal.aborted ? "Read-only tool call was cancelled" : "Read-only tool outcome is unknown";
        await this.bridge.recordResult({ ...call, text: failure, failed: true });
        throw new Error(failure);
      }
      try {
        await this.bridge.recordUnknown(exec.callId);
      } finally {
        this.bridge.stop();
      }
      throw new Error("Remote tool outcome is unknown; turn stopped without replay");
    }
    let text: string;
    let failed = !!result.isError;
    try {
      text = toolText(result, tool.name);
    } catch (error) {
      text = `Sandbox tool returned an unsupported result: ${(error as Error).message}`;
      failed = true;
    }
    const changed = !isDeepStrictEqual(original, effective);
    await this.bridge.recordResult({ ...call, text, failed });
    if (changed)
      exec.deferContext(createUserMessage({
        source: { kind: "user" },
        content: [{
          type: "text",
          text: `OMA approval changed ${tool.name} arguments to ${JSON.stringify(effective)}. The returned result is for these approved arguments.`,
        }],
      }));
    if (failed) throw new Error(text || "Remote tool failed");
    return text;
  }

  private async callWithRetry(tool: RemoteTool, args: Payload, signal: AbortSignal): Promise<Payload> {
    try {
      return await this.sandbox.call(tool, args, signal);
    } catch (error) {
      if (signal.aborted) throw error;
      return await this.sandbox.call(tool, args, signal);
    }
  }
}
