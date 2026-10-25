import { billingError } from "../src/llm-ai-sdk/stream.ts";
import { describe, expect, test } from "bun:test";
import { LlmError } from "@deepseek-ai/dsh-llm";
import { checkSessionBudget, sessionBilling } from "../src/billing.ts";
import { terminalOutput } from "../src/ccrv2/native-output.ts";

const amount = (value: string) => ({ amount: value, currency: "USD" });
const context = () => ({ budget: { type: "limit", max_list_cost: amount("100") }, usage: amount("25"),
  remaining: amount("75"), budget_reached: false, observed_at: "2026-10-22T00:00:00Z", pricing: { "narrows/model": null },
  calculation_policy: "server_authoritative" });

describe("会话预算与价格", () => {
  test("原生终止输出及恢复重放保留预算暂停与缺价原因", () => {
    expect(terminalOutput("paused", { kind: "error", error: new LlmError("budget", "SESSION_BUDGET_REACHED") })).toEqual([]);
    expect(terminalOutput("unpriced", { kind: "error", error: new LlmError("pricing", "MODEL_PRICE_UNAVAILABLE") }))
      .toContainEqual(expect.objectContaining({ type: "system.message", content: [{ type: "text", text: "Model pricing is unavailable. Retry after pricing recovers." }] }));
    expect(terminalOutput("completed", { kind: "completed" }))
      .toContainEqual(expect.objectContaining({ type: "result", is_error: false }));
  });
  test("拒绝错误金额和损坏价格", () => {
    expect(() => sessionBilling({ ...context(), usage: amount("-1") })).toThrow();
    expect(() => sessionBilling({ ...context(), pricing: { model: {} } })).toThrow();
  });
  test("预算触顶停止请求，更新预算后可以继续", () => {
    expect(() => checkSessionBudget(sessionBilling({ ...context(), budget_reached: true }))).toThrow(LlmError);
    expect(() => checkSessionBudget(sessionBilling(context()))).not.toThrow();
  });
  test("识别两种不可自动重试的后端错误", () => {
    for (const code of ["session_budget_reached", "model_price_unavailable"])
      expect(billingError(JSON.stringify({ error: { code, message: "blocked" } }), 400)?.code).toBe(code.toUpperCase());
    expect(billingError("broken")).toBeUndefined();
    expect(billingError('{"error":{"code":"other"}}')).toBeUndefined();
  });
  test("保留空预算和未知价格，兼容旧后端", () => {
    expect(sessionBilling(undefined)).toBeUndefined();
    const parsed = sessionBilling({ ...context(), budget: null, remaining: null });
    expect(parsed?.budget).toBeNull();
    expect(parsed?.pricing["narrows/model"]).toBeNull();
  });
});
