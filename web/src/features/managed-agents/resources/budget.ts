import { type DeploymentApiResponse, type ManagedEntityFormValues, type SessionApiResponse } from '../types';

/** Budget wire shape: integer-cent USD amount as a string (CMA-compatible). */
export type UsdBudgetWire = {
  type: 'limit';
  max_list_cost: { amount: string; currency: string };
};

export type CreditsBudgetWire = {
  type: 'limit';
  max_credit_cost: string;
};

export type BudgetWire = UsdBudgetWire | CreditsBudgetWire;

export type BudgetParseResult = { ok: true; cents: number | null } | { ok: false };

/**
 * Parses a dollars input into integer cents. Empty means "no budget".
 * Accepts plain integers/decimals (e.g. "25", "12.5", "0.99"); rejects
 * negatives, exponents, and extra precision.
 */
export function parseBudgetUsdInput(input: string): BudgetParseResult {
  const value = input.trim();
  if (!value) {
    return { ok: true, cents: null };
  }
  if (!/^\d+(?:\.\d{1,2})?$/.test(value)) {
    return { ok: false };
  }
  const [whole, fraction = ''] = value.split('.');
  const cents = Number(whole) * 100 + Number((fraction + '00').slice(0, 2) || '0');
  if (!Number.isSafeInteger(cents) || cents <= 0) {
    return { ok: false };
  }
  return { ok: true, cents };
}

export function budgetWireBody(cents: number): UsdBudgetWire {
  return { type: 'limit', max_list_cost: { amount: String(cents), currency: 'USD' } };
}

export function budgetCreditsWireBody(cents: number): CreditsBudgetWire {
  return { type: 'limit', max_credit_cost: (cents / 100).toFixed(2) };
}

export function budgetCreateBody(values: ManagedEntityFormValues): BudgetWire | undefined {
  if (values.budgetMode === 'none') {
    return undefined;
  }
  const parsed = parseBudgetUsdInput(values.budgetUsd);
  if (!parsed.ok || parsed.cents === null) {
    return undefined;
  }
  return values.budgetMode === 'credits' ? budgetCreditsWireBody(parsed.cents) : budgetWireBody(parsed.cents);
}

export function budgetUpdateBody(values: ManagedEntityFormValues): BudgetWire | null | undefined {
  const parsed = parseBudgetUsdInput(values.budgetUsd);
  const original = parseBudgetUsdInput(values.originalBudgetUsd);
  if (parsed.ok && original.ok && parsed.cents === original.cents) return undefined;
  if (!parsed.ok || parsed.cents === null) return null;
  return values.budgetMode === 'credits' ? budgetCreditsWireBody(parsed.cents) : budgetWireBody(parsed.cents);
}

export function budgetValid(values: ManagedEntityFormValues): boolean {
  return parseBudgetUsdInput(values.budgetUsd).ok;
}

function entityBudgetCreditsCents(entity: unknown): number | null {
  const budget = (entity as { budget?: unknown } | null | undefined)?.budget;
  if (!budget || typeof budget !== 'object') {
    return null;
  }
  const raw = (budget as { max_credit_cost?: unknown }).max_credit_cost;
  if (typeof raw !== 'string') {
    return null;
  }
  const parsed = parseBudgetUsdInput(raw);
  return parsed.ok ? parsed.cents : null;
}

function entityBudgetCents(entity: unknown): number | null {
  const budget = (entity as { budget?: unknown } | null | undefined)?.budget;
  if (!budget || typeof budget !== 'object') {
    return null;
  }
  const maxListCost = (budget as { max_list_cost?: { amount?: unknown } }).max_list_cost;
  const amount = Number(maxListCost?.amount);
  return Number.isSafeInteger(amount) && amount > 0 ? amount : null;
}

export function entityBudgetUsdInput(entity: unknown): string {
  const cents = entityBudgetCents(entity) ?? entityBudgetCreditsCents(entity);
  return cents === null ? '' : formatUsdCentsPlain(cents);
}

export function formatUsdCents(cents: number): string {
  return `$${formatUsdCentsPlain(cents)}`;
}

function formatUsdCentsPlain(cents: number): string {
  return (cents / 100).toFixed(2).replace(/\.00$/, '');
}

export type SessionBudgetState = {
  budgetCents: number;
  spentCents: number;
  reached: boolean;
};

export function sessionBudgetState(session: SessionApiResponse): SessionBudgetState | null {
  const budgetCents = entityBudgetCents(session);
  if (budgetCents === null) {
    return null;
  }
  const usage = (session.usage ?? null) as { list_cost?: { amount?: unknown } } | null;
  const spentCents = Number(usage?.list_cost?.amount ?? 0);
  return {
    budgetCents,
    spentCents: Number.isSafeInteger(spentCents) && spentCents > 0 ? spentCents : 0,
    reached: spentCents >= budgetCents,
  };
}

export function entityBudgetDisplay(entity: DeploymentApiResponse | SessionApiResponse): string {
  const usdCents = entityBudgetCents(entity);
  if (usdCents !== null) return formatUsdCents(usdCents);
  const creditCents = entityBudgetCreditsCents(entity);
  if (creditCents !== null) return `${formatUsdCentsPlain(creditCents)} Credits`;
  return '—';
}

export function isBudgetReachedError(message: string | null): boolean {
  return Boolean(message?.startsWith('session budget reached:'));
}
