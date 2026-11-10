import { readTranscriptPage } from "./transcript-page.ts";
import { parseInput } from "./input-stream.ts";
import type { Input } from "./wire.ts";
import { validateModels, validateToolPermissions } from "../assignment.ts";
import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";
import { DsrError, decimal, object, request, type Transport } from "./http.ts";
import type {
  Claim,
  Registration,
  Finish,
  PermissionRequest,
  Permission,
  Delivery,
  DeliveryResult,
  Transcript,
  TranscriptPage,
  PublicEvent,
} from "./wire.ts";

export class DsrClient {
  // 该会话所有接口的路径前缀。会话 id 先做 URL 编码，避免特殊字符。
  readonly root: string;
  // 冻结一份 claim，防止运行期间被外部修改导致 token/epoch 不一致。
  readonly claim: Readonly<Claim>;
  // 正在进行中的 register 请求。多个调用共享它，避免并发重复注册。
  private registration?: Promise<Registration>;
  // 会话恢复时用它判断某个输入是否已经被用户取消。
  private readonly cancelled = new Set<string>();

  constructor(
    readonly transport: Transport,
    claim: Readonly<Claim>,
  ) {
    this.claim = Object.freeze({ ...claim });
    // epoch 必须是正十进制字符串；否则说明 claim 本身已损坏。
    decimal(claim.worker_epoch, true);
    this.root = `/v1/dsr/sessions/${encodeURIComponent(claim.code_session_id)}`;
  }

  /**
   * 注册会话并获取租约参数（幂等 / 可重试）。
   *
   * 并发语义：第一个调用发出请求，其余调用直接复用同一个 Promise。
   * 请求结束后清空缓存，失败后下次调用可以重新尝试。
   *
   * @example
   *   const [a, b] = await Promise.all([client.register(), client.register()]);
   *   // 只会发出一次 POST /register，a === b 的内容相同
   */
  async register(signal?: AbortSignal): Promise<Registration> {
    if (this.registration) return this.registration;
    this.registration = this.registerOnce(signal).finally(() => {
      this.registration = undefined;
    });
    return this.registration;
  }

  /**
   * 真正的 register 请求与响应校验。
   *
   * 必须满足：
   *   - 回显的 epoch 与 claim 一致（防止认领后被别人顶掉）；
   *   - protocol 为 "1"；
   *   - observability_enabled 是布尔值；
   *   - lease 的三个时间都是正安全整数；
   *   - heartbeat_interval * 1.1 + 5s 必须小于 ttl，否则心跳来不及，
   *     租约会周期性过期（这里提前拒绝这种不合理的服务端配置）。
   *
   * @example
   *   // 合法响应
   *   {epoch: "7", protocol: "1", observability_enabled: true,
   *    lease: {ttl_ms: 30000, grace_ms: 10000, heartbeat_interval_ms: 10000}}
   *
   *   // lease.ttl_ms = 10000, heartbeat_interval_ms = 10000
   *   // -> 10000*1.1+5000 >= 10000 -> invalid_response
   */
  private async registerOnce(signal?: AbortSignal): Promise<Registration> {
    const response = await request(this.transport, {
      path: `${this.root}/register`,
      token: this.claim.session_token,
      method: "POST",
      body: {},
      signal,
    });
    const value = object(await response.json());
    const lease = object(value.lease);
    if (
      value.epoch !== this.claim.worker_epoch ||
      value.protocol !== "1" ||
      typeof value.observability_enabled !== "boolean" ||
      ![lease.ttl_ms, lease.grace_ms, lease.heartbeat_interval_ms].every(
        (value) =>
          typeof value === "number" && Number.isSafeInteger(value) && value > 0,
      ) ||
      (lease.heartbeat_interval_ms as number) * 1.1 + 5000 >=
        (lease.ttl_ms as number)
    )
      throw new DsrError(0, "invalid_response");
    const context = object(value.context);
    if (
      typeof context.model !== "string" ||
      !context.model ||
      typeof context.cwd !== "string" ||
      !context.cwd.startsWith("/") ||
      !Array.isArray(context.runtime_tools) ||
      context.runtime_tools.some((tool) => typeof tool !== "string")
    )
      throw new DsrError(0, "invalid_response");
    object(context.models);
    object(context.mcp_config);
    validateModels(context.models);
    validateToolPermissions(context.runtime_tool_permissions);
    const children = object(context.subagent_contexts);
    for (const raw of Object.values(children)) {
      const child = object(raw);
      object(child.mcp_config);
      validateToolPermissions(child.runtime_tool_permissions);
    }
    return value as unknown as Registration;
  }

  // 等待 register 完成。json() 在每个请求前调用它，保证不会在未注册时调接口。
  async ready(): Promise<void> {
    await this.registration;
  }

  private async response(
    path: string,
    method = "GET",
    body?: unknown,
    signal?: AbortSignal,
    headers?: Record<string, string>,
  ): Promise<Response> {
    await this.ready();
    const send = () =>
      request(this.transport, {
        path: `${this.root}${path}`,
        method,
        body,
        token: this.claim.session_token,
        signal,
        headers,
      });
    let response: Response;
    try {
      response = await send();
    } catch (error) {
      if (!(error instanceof DsrError) || error.code !== "lease_expired")
        throw error;
      await this.register(signal);
      response = await send();
    }
    return response;
  }

  private async json(
    path: string,
    method = "GET",
    body?: unknown,
    signal?: AbortSignal,
    headers?: Record<string, string>,
  ): Promise<Record<string, unknown>> {
    return object(
      await (await this.response(path, method, body, signal, headers)).json(),
    );
  }

  /**
   * 续租。返回新的 lease_expires_at。
   *
   * @example
   *   const expiresAt = await client.heartbeat();
   *   // "2026-11-04T12:00:40Z"
   */
  async heartbeat(signal?: AbortSignal): Promise<string> {
    const response = await this.json("/heartbeat", "POST", {}, signal);
    if (
      typeof response.lease_expires_at !== "string" ||
      !Number.isFinite(Date.parse(response.lease_expires_at))
    )
      throw new DsrError(0, "invalid_response");
    return response.lease_expires_at;
  }

  async delivery(
    updates: Delivery[],
    signal?: AbortSignal,
  ): Promise<DeliveryResult> {
    const response = await this.json(
      "/inputs/delivery",
      "POST",
      { updates },
      signal,
    );
    if (
      !Number.isSafeInteger(response.applied) ||
      !Number.isSafeInteger(response.ignored) ||
      !Array.isArray(response.results) ||
      response.results.length !== updates.length
    )
      throw new DsrError(0, "invalid_response");
    for (const [index, raw] of response.results.entries()) {
      const item = object(raw);
      if (
        item.id !== updates[index]!.id ||
        item.status !== updates[index]!.status ||
        !["applied", "ignored"].includes(String(item.outcome))
      )
        throw new DsrError(0, "invalid_response");
    }
    return response as unknown as DeliveryResult;
  }

  async claimInput(
    signal?: AbortSignal,
  ): Promise<Extract<Input, { lane: "task" }> | null> {
    const response = await this.json("/inputs/claim", "POST", {}, signal);
    if (response.input === null) return null;
    const input = parseInput(JSON.stringify(response.input));
    if (input.lane !== "task") throw new DsrError(0, "invalid_response");
    if (response.cancel_requested_at !== undefined) {
      if (
        typeof response.cancel_requested_at !== "string" ||
        !Number.isFinite(Date.parse(response.cancel_requested_at))
      )
        throw new DsrError(0, "invalid_response");
      this.cancelled.add(input.id);
    }
    return input;
  }

  // 某个输入是否已被服务器/用户取消。
  inputCancelled(id: string): boolean {
    return this.cancelled.has(id);
  }

  /**
   * 提交某个输入的最终结论。
   *
   * 服务器会回显 result 与 failure_code，用于幂等校验：
   * 如果这个输入之前已经 finish 过且结论不同，说明本地状态已经错乱，
   * 抛 finish_idempotency_mismatch，而不是静默覆盖。
   *
   * @example
   *   await client.finish("in_1", {result: "success"});
   *   await client.finish("in_2", {result: "failed", failure_code: "execution_failed"});
   */
  async finish(
    id: string,
    completion: Finish,
    signal?: AbortSignal,
  ): Promise<void> {
    const response = await this.json(
      `/inputs/${encodeURIComponent(id)}/finish`,
      "POST",
      completion,
      signal,
    );
    if (
      response.result !== completion.result ||
      response.failure_code !==
        ("failure_code" in completion ? completion.failure_code : undefined)
    )
      throw new DsrError(0, "finish_idempotency_mismatch");
  }

  async appendRecords(
    id: string,
    fromOffset: string,
    records: readonly (SessionHeader | SessionEvent)[],
    parent: string | null,
    signal?: AbortSignal,
  ): Promise<string> {
    decimal(fromOffset);
    const query = new URLSearchParams({ from_offset: fromOffset });
    if (parent !== null) query.set("parent_dsh_session_id", parent);
    const response = await this.response(
      `/transcripts/${encodeURIComponent(id)}/records?${query}`,
      "POST",
      records.map((record) => JSON.stringify(record) + "\n").join(""),
      signal,
      { "Content-Type": "application/x-ndjson" },
    );
    return decimal(object(await response.json()).next_offset);
  }

  /**
   * 分页列出本会话的所有 transcript（根会话 + 子线程）。
   *
   * 响应校验比较严格：
   *   - data 内 id 必须严格递增（服务器按 id 排序）；
   *   - next_after 必须等于本页最后一个 id，且不能回退；
   *   - 避免分页实现错误导致死循环或漏读。
   *
   * @example
   *   [{dsh_session_id: "ses_child", parent_dsh_session_id: "ses_root", ...},
   *    {dsh_session_id: "ses_root", ...}]
   */
  async listTranscripts(signal?: AbortSignal): Promise<Transcript[]> {
    const result: Transcript[] = [];
    let after: string | null = null;
    do {
      const response = await this.json(
        `/transcripts?limit=200${after === null ? "" : `&after=${encodeURIComponent(after)}`}`,
        "GET",
        undefined,
        signal,
      );
      if (
        !Array.isArray(response.data) ||
        (response.next_after !== null &&
          typeof response.next_after !== "string")
      )
        throw new DsrError(0, "invalid_response");
      let previous: string | null = after;
      for (const raw of response.data) {
        const row = object(raw);
        decimal(row.next_offset, true);
        if (
          typeof row.dsh_session_id !== "string" ||
          !row.dsh_session_id ||
          (previous !== null && row.dsh_session_id <= previous)
        )
          throw new DsrError(0, "invalid_response");
        previous = row.dsh_session_id;
        result.push(row as unknown as Transcript);
      }
      const next = response.next_after as string | null;
      if (
        next !== null &&
        (response.data.length === 0 ||
          next !== previous ||
          (after !== null && next <= after))
      )
        throw new DsrError(0, "invalid_response");
      after = next;
    } while (after !== null);
    return result;
  }

  async readTranscript(
    id: string,
    from = "0",
    signal?: AbortSignal,
  ): Promise<TranscriptPage> {
    decimal(from);
    const response = await this.response(
      `/transcripts/${encodeURIComponent(id)}/records?from_offset=${from}`,
      "GET",
      undefined,
      signal,
    );
    return readTranscriptPage(response, from, id);
  }

  /**
   * 发起工具审批请求。
   *
   * @example
   *   const permission = await client.permission({
   *     input_id: "in_1", turn: "2", dsh_session_id: "ses_root",
   *     tool_use_id: "call_1", tool: {kind: "builtin", name: "bash"},
   *     input: {command: "ls"},
   *   });
   *   // {request_id: "pr_1", state: "pending"}
   */
  async permission(
    input: PermissionRequest,
    signal?: AbortSignal,
  ): Promise<Permission> {
    return this.parsePermission(
      await this.json("/permission-requests", "POST", input, signal),
    );
  }

  // 关闭一个不再需要的审批请求（例如工具调用已被中断）。
  async cancelPermission(
    id: string,
    input: {
      dsh_session_id: string;
      tool_use_id: string;
      input_id?: string;
    },
    signal?: AbortSignal,
  ): Promise<Permission> {
    return this.parsePermission(
      await this.json(
        `/permission-requests/${encodeURIComponent(id)}/cancel`,
        "POST",
        input,
        signal,
      ),
    );
  }

  /**
   * 校验审批响应：request_id 非空、state 合法；
   * resolved 时 decision.behavior 必须是 allow/deny/external_result。
   */
  private parsePermission(response: Record<string, unknown>): Permission {
    if (
      typeof response.request_id !== "string" ||
      !response.request_id ||
      !["pending", "resolved", "cancelled", "closed"].includes(
        String(response.state),
      )
    )
      throw new DsrError(0, "invalid_response");
    if (
      response.state === "resolved" &&
      !["allow", "deny", "external_result"].includes(
        String(object(response.decision).behavior),
      )
    )
      throw new DsrError(0, "invalid_response");
    return response as unknown as Permission;
  }

  /**
   * 上报公共事件（消息、工具结果、子线程状态等）。
   *
   * 服务器会回执 accepted + duplicates；两者之和必须等于
   * “非 preview 事件”的数量（preview 事件是流式预览，不参与持久化计数）。
   *
   * @example
   *   await client.publish([
   *     {id: "u1", type: "agent.message", content: [...]},
   *     {id: "u2", type: "tool.result", tool_use_id: "c1", tool: {...}, is_error: false, content: [...]},
   *   ]);
   *   // 若 u1 之前已上报过：{accepted: 1, duplicates: 1}
   */
  async publish(events: PublicEvent[], signal?: AbortSignal): Promise<void> {
    const response = await this.json(
      "/public-events",
      "POST",
      { events },
      signal,
    );
    const count = events.filter(
      (event) => !event.type.startsWith("preview."),
    ).length;
    if (
      !Number.isSafeInteger(response.accepted) ||
      !Number.isSafeInteger(response.duplicates) ||
      (response.accepted as number) + (response.duplicates as number) !== count
    )
      throw new DsrError(0, "invalid_response");
  }
}
