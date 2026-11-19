import { isDeepStrictEqual } from "node:util";
import { createUserMessage, type ContentBlock } from "@deepseek-ai/dsh-llm";
import { validateJsonSchemaValue, type JsonSchemaNode } from "@deepseek-ai/dsh-tools";
import { object, type Payload } from "../json.ts";
import { approveToolCall } from "./tool-approvals.ts";
import { toolText } from "./tools.ts";
import {
  RetryableInputError,
  UnknownSandboxOutcomeError,
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
    readonly threadId?: string,
  ) {}

  execute(tool: RemoteTool, original: Payload, exec: ToolExecutionContext): Promise<string>;
  execute(tool: RemoteTool, original: Payload, exec: ToolExecutionContext, content: (result: Payload) => Promise<ContentBlock[]>): Promise<ContentBlock[]>;
  async execute(tool: RemoteTool, original: Payload, exec: ToolExecutionContext, content?: (result: Payload) => Promise<ContentBlock[]>): Promise<string | ContentBlock[]> {
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
        ?? await this.sandbox.call(tool, effective, exec.signal, this.threadId, exec.callId), readOnly);
      if (result.errorCode === "TOOL_OUTCOME_UNKNOWN") throw new UnknownSandboxOutcomeError(toolText(result));
    } catch (error) {
      if (error instanceof UnknownSandboxOutcomeError) throw error;
      const userInterrupted = exec.signal.aborted && exec.signal.reason?.kind === "user";
      const shutdownInterrupted = exec.signal.aborted && isShutdownCancel(exec.signal.reason);
 if (shutdownInterrupted) throw error;
      if (readOnly && !userInterrupted || error instanceof ToolNotForwardedError) {
        const failure = error instanceof ToolNotForwardedError ? error.message
          : exec.signal.aborted ? "Read-only tool call was cancelled" : "Read-only tool outcome is unknown";
        throw new Error(failure);
      }
      if (userInterrupted) {
        const text = "Tool call interrupted.";
        result = { isError: true, content: [{ type: "text", text }] };
      } else {
        throw new Error("Remote tool outcome is unknown. Do not retry blindly; verify external state before repeating operations with side effects.");
      }
    }
    let output: string | ContentBlock[];
    let failed = !!result.isError;
    try {
      output = content && !result.isError ? await content(result) : toolText(result);
    } catch (error) {
      exec.signal.throwIfAborted();
      if (error instanceof RetryableInputError) throw error;
      output = `Sandbox tool returned an unsupported result: ${(error as Error).message}`;
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
    if (failed) throw new Error(typeof output === "string" && output || "Remote tool failed");
    return output;
  }

}
