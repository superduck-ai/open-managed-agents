import { isDeepStrictEqual } from "node:util";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { validateJsonSchemaValue, type JsonSchemaNode } from "@deepseek-ai/dsh-tools";
import { object, type Payload } from "../json.ts";
import { approveToolCall } from "./tool-approvals.ts";
import { toolText } from "./tools.ts";
import {
  ToolNotForwardedError,
  isShutdownCancel,
  type ApprovalDecision,
  type RemoteTool,
  type SandboxExecutor,
  type ToolExecutionBridge,
  type ToolExecutionContext,
  type ToolPermissions,
} from "./contracts.ts";

export class ToolExecution {
  constructor(
    readonly sandbox: SandboxExecutor,
    readonly bridge: ToolExecutionBridge,
    readonly permissions?: ToolPermissions,
  ) {}

  async execute(tool: RemoteTool, original: Payload, exec: ToolExecutionContext): Promise<string> {
    const validate = (args: Payload) => {
      const errors = validateJsonSchemaValue(tool.inputSchema as JsonSchemaNode, args);
      if (errors.length)
        throw new Error(`Invalid ${tool.name} arguments: ${errors.join("; ")}`);
    };
    validate(original);
    const decision: ApprovalDecision = this.permissions?.[tool.permissionName ?? tool.remoteName] === "allow"
      ? { allowed: true }
      : await approveToolCall(this.bridge, { callId: exec.callId, toolName: tool.name, input: original }, exec.signal);
    if (!decision.allowed)
      throw new Error(decision.reason || "Tool use denied by OMA");
    const effective = decision.input === undefined ? original : object(decision.input);
    validate(effective);
    exec.signal.throwIfAborted();
    const readOnly = !!tool.readOnly;
    const changed = !isDeepStrictEqual(original, effective);
    if (changed) await this.bridge.approvalChanged(tool.name, exec.callId, effective);
    if (!decision.externalResult) await this.sandbox.provision?.(exec.signal);
    exec.signal.throwIfAborted();
    let result: Payload;
    try {
      result = await this.bridge.runTool(exec.callId, async () => decision.externalResult
        ?? (readOnly ? await this.callWithRetry(tool, effective, exec.signal) : await this.sandbox.call(tool, effective, exec.signal)), readOnly);
    } catch (error) {
      const userInterrupted = exec.signal.aborted && exec.signal.reason?.kind === "user";
      const shutdownInterrupted = exec.signal.aborted && isShutdownCancel(exec.signal.reason);
      if (readOnly && !userInterrupted || error instanceof ToolNotForwardedError) {
        const failure = error instanceof ToolNotForwardedError ? error.message
          : exec.signal.aborted ? "Read-only tool call was cancelled" : "Read-only tool outcome is unknown";
        throw new Error(failure);
      }
      if (userInterrupted || shutdownInterrupted) {
        const text = userInterrupted ? "Tool call interrupted."
          : "Tool call interrupted because the Brain worker shut down; its outcome is unknown.";
        result = { isError: true, content: [{ type: "text", text }] };
      } else {
        throw new Error("Remote tool outcome is unknown. Do not retry blindly; verify external state before repeating operations with side effects.");
      }
    }
    let text: string;
    let failed = !!result.isError;
    try {
      text = toolText(result, tool.name);
    } catch (error) {
      text = `Sandbox tool returned an unsupported result: ${(error as Error).message}`;
      failed = true;
    }
    if (changed) {
      exec.deferContext(createUserMessage({
        source: { kind: "user" },
        content: [{
          type: "text",
          text: `OMA approval changed ${tool.name} arguments to ${JSON.stringify(effective)}. The returned result is for these approved arguments.`,
        }],
      }));
    }
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
