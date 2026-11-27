import { LlmError } from "@deepseek-ai/dsh-llm";

type CostAmount = { amount: string; currency: "USD" };
type Rates = Record<"input" | "output" | "cache_read" | "cache_write" | "cache_write_5m" | "cache_write_1h", string | null>;

export interface ModelPrice {
  source: "openrouter" | "narrows";
  source_model: string;
  version: string;
  fetched_at: string;
  priced_at: string;
  source_currency?: string;
  exchange_rate?: string;
  rates: Rates;
  overrides?: Array<{
    rates: Rates;
    min_prompt_tokens?: number; max_prompt_tokens?: number;
    utc_start?: number; utc_end?: number; utc_days?: string[];
  }>;
}

export interface SessionBilling {
  budget: { type: "limit"; max_list_cost: CostAmount } | null;
  usage: CostAmount;
  remaining: CostAmount | null;
  budget_reached: boolean;
  observed_at: string;
  pricing: Record<string, ModelPrice | null>;
  calculation_policy: string;
}

export function sessionBilling(value: unknown): SessionBilling | undefined {
  if (value === undefined) return undefined;
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid session billing");
  const billing = value as SessionBilling;
  if (typeof billing.budget_reached !== "boolean" || typeof billing.observed_at !== "string"
    || typeof billing.calculation_policy !== "string" || !billing.pricing || typeof billing.pricing !== "object" || Array.isArray(billing.pricing)
    || !validAmount(billing.usage) || (billing.remaining !== null && !validAmount(billing.remaining))
    || (billing.budget !== null && (billing.budget?.type !== "limit" || !validAmount(billing.budget.max_list_cost))))
    throw new Error("Invalid session billing");
  for (const price of Object.values(billing.pricing)) {
    if (price !== null && (!price || !["openrouter", "narrows"].includes(price.source)
      || typeof price.source_model !== "string" || typeof price.version !== "string"
      || typeof price.fetched_at !== "string" || !price.rates || typeof price.rates !== "object"))
      throw new Error("Invalid model price");
  }
  return billing;
}

function validAmount(value: CostAmount | undefined): boolean {
  return !!value && value.currency === "USD" && typeof value.amount === "string" && /^(0|[1-9][0-9]*)$/.test(value.amount);
}

export function checkSessionBudget(billing: SessionBilling | undefined): void {
  if (billing?.budget_reached) throw new LlmError("Session budget reached. Increase or remove the budget to continue.", "SESSION_BUDGET_REACHED");
}
