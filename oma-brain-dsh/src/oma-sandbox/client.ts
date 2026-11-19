import { createHash } from "node:crypto";
import { HttpError, OmaApi } from "../api.ts";
import { object, type Payload } from "../json.ts";
import { normalizeSandboxResult } from "./output.ts";
import { isShutdownCancel, UnknownSandboxOutcomeError, ToolNotForwardedError, type SandboxExecutor, type RemoteTool } from "../runtime/contracts.ts";
import { sleep } from "../ccrv2/retry.ts";

export const CANCELLATION_UNCONFIRMED = "Cancellation could not be confirmed. Verify external state before repeating operations with side effects.";

export class SandboxCalls implements SandboxExecutor {
  constructor(readonly api: OmaApi, readonly sessionId: string) {}

  async provision(signal: AbortSignal): Promise<void> { signal.throwIfAborted(); }

  private path(callId: string, threadId = "root"): string {
    return `/v2/ccr-sessions/${encodeURIComponent(this.sessionId)}/threads/${encodeURIComponent(threadId)}/tool-calls${callId ? `/${encodeURIComponent(callId)}` : ""}`;
  }

  private async request(path: string, method: string, body: unknown, signal?: AbortSignal): Promise<Payload> {
    for (;;) {
      try {
        return await this.api.request(path, this.api.assignment(this.sessionId).ingress_token, method, body, this.api.traceHeaders(this.sessionId), signal, 35_000);
      } catch (error) {
        signal?.throwIfAborted();
        if (error instanceof HttpError && error.status < 500) throw error;
        await sleep(500, signal);
      }
    }
  }

  async wait(callId: string, signal: AbortSignal, threadId?: string, pending?: () => void): Promise<Payload> {
    for (;;) {
      const call = await this.request(`${this.path(callId, threadId)}?wait_ms=25000`, "GET", undefined, signal);
      if (["pending", "dispatched", "running", "cancel_requested", "finalizing"].includes(call.state)) {
        pending?.();
        pending = undefined;
        continue;
      }
      if (call.result) return { ...object(call.result), isError: call.state !== "succeeded" };
      const text = call.state === "cancelled" ? "Tool call interrupted."
        : call.state === "expired" ? "Tool call expired before execution."
        : "Remote tool outcome is unknown. Do not retry blindly; verify external state before repeating operations with side effects.";
      return { isError: true, errorCode: call.state === "lost" ? "TOOL_OUTCOME_UNKNOWN" : undefined, content: [{ type: "text", text }] };
    }
  }

  async call(tool: RemoteTool, args: Payload, signal: AbortSignal, threadId?: string, callId?: string, resume = false): Promise<Payload> {
    if (!callId) throw new Error("Sandbox call requires a native call ID");
    signal.throwIfAborted();
    if (tool.remoteName === "friday") {
      const argv = args.args;
      if (threadId && threadId !== "root" || Object.keys(args).length !== 1 || !Array.isArray(argv)
        || !["app", "mart", "event", "schema", "meta", "chart", "property", "dashboard", "analysis", "sql", "log"].includes(argv[0])
        || argv.some(arg => typeof arg !== "string" || arg.includes("\0") || /^--(api-key|base-url|proxy-auth|skip-ssl-verify)(=|$)/.test(arg)))
        throw new ToolNotForwardedError("Friday 查询参数无效");
      let placeholder = "";
      const help = ["--help", "-h"].includes(argv.at(-1)) && argv.slice(0, -1).every(arg => /^[a-z][a-z-]*$/.test(arg));
      if (resume && help) throw new ToolNotForwardedError("帮助命令没有待恢复的授权");
      if (!help) {
        const path = this.path(callId, threadId) + "/friday-oauth" + (resume ? "?resume=1" : "");
        let grant: Payload;
        try {
          grant = await this.api.request(path, this.api.assignment(this.sessionId).ingress_token, "POST", {},
            { ...this.api.traceHeaders(this.sessionId), "X-OMA-Friday-Input": createHash("sha256").update(JSON.stringify(argv)).digest("hex") }, signal, 0);
        } catch (error) {
          signal.throwIfAborted();
          if (resume && (!(error instanceof HttpError) || error.status >= 500)) throw error;
          throw new ToolNotForwardedError("Friday 授权检查失败，CLI 尚未启动。", { cause: error });
        }
        if (grant.authorized !== true || typeof grant.placeholder !== "string" || !grant.placeholder)
          throw new ToolNotForwardedError("Friday 授权未就绪");
        placeholder = grant.placeholder;
      }
      const quoted = argv.map(arg => "'" + arg.replaceAll("'", "'\\''") + "'").join(" ");
      tool = { ...tool, remoteName: "bash" };
      args = {
        command: 'set -e; FRIDAY_CLI_CONFIG_DIR="$(mktemp -d)"; export FRIDAY_CLI_CONFIG_DIR; trap \'rm -rf "$FRIDAY_CLI_CONFIG_DIR"\' EXIT; unset FRIDAY_API_KEY OMA_FRIDAY_OAUTH_PROVIDER OMA_TOOL_CALL_ID; "${OMA_FRIDAY_EXECUTABLE:-friday}" ' + quoted,
        env: { FRIDAY_BASE_URL: "https://friday-tx.seewo.com", FRIDAY_PROXY_AUTH: "0", FRIDAY_SKIP_SSL_VERIFY: "false", FRIDAY_4A_TOKEN: placeholder },
      };
    }
    try {
      await this.request(this.path("", threadId), "POST", { call_id: callId, tool: tool.remoteName, input: args, timeout_ms: sandboxCallTimeoutMs(tool, args) }, signal);
      return normalizeSandboxResult(await this.wait(callId, signal, threadId), tool.name);
    } catch (error) {
      if (signal.aborted && signal.reason?.kind === "user") {
        try {
          return normalizeSandboxResult(await this.cancel(callId, threadId), tool.name);
        } catch (cause) {
          throw new UnknownSandboxOutcomeError(CANCELLATION_UNCONFIRMED,{cause});
        }
      }
      if (signal.aborted && isShutdownCancel(signal.reason)) throw error;
      if (error instanceof HttpError && [400, 403, 413].includes(error.status))
        throw new ToolNotForwardedError(`Sandbox tool call rejected with HTTP ${error.status}`);
      throw error;
    }
  }

  async peek(callId: string, signal: AbortSignal, threadId?: string): Promise<Payload> {
    return await this.request(`${this.path(callId, threadId)}?wait_ms=0`, "GET", undefined, signal);
  }

  async cancel(callId: string, threadId?: string, shutdown?: AbortSignal): Promise<Payload> {
    const cancellation = shutdown ? AbortSignal.any([shutdown, AbortSignal.timeout(90_000)]) : AbortSignal.timeout(90_000);
    await this.request(`${this.path(callId, threadId)}/cancel`, "POST", {}, cancellation);
    return await this.wait(callId, cancellation, threadId);
  }

  async close(): Promise<void> {}
}

export function sandboxCallTimeoutMs(tool: RemoteTool, args: Payload): number {
  if (tool.remoteName !== "bash") return 180_000;
  const requested = Number.isFinite(args.timeout) && args.timeout > 0 ? args.timeout : 120;
  return Math.min(requested, 600) * 1000 + 30_000;
}
