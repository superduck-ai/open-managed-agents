import { type DeploymentApiResponse, type ManagedEntityFormValues, type SessionApiResponse } from '../types';

/** Budget wire shape: integer-cent USD amount as a string (CMA-compatible). */
export type BudgetWire = {
  type: 'limit';
  max_list_cost: { amount: string; currency: string };
};

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

export function budgetWireBody(cents: number): BudgetWire {
  return { type: 'limit', max_list_cost: { amount: String(cents), currency: 'USD' } };
}

/** Body fragment for create requests: budget present only when set. */
export function budgetCreateBody(values: ManagedEntityFormValues): BudgetWire | undefined {
  const parsed = parseBudgetUsdInput(values.budgetUsd);
  return parsed.ok && parsed.cents !== null ? budgetWireBody(parsed.cents) : undefined;
}

/** Body fragment for update requests: sent only when edited, null clears. */
export function budgetUpdateBody(values: ManagedEntityFormValues): BudgetWire | null | undefined {
  if (!values.budgetChanged) {
    return undefined;
  }
  const parsed = parseBudgetUsdInput(values.budgetUsd);
  return parsed.ok && parsed.cents !== null ? budgetWireBody(parsed.cents) : null;
}

export function budgetValid(values: ManagedEntityFormValues): boolean {
  return parseBudgetUsdInput(values.budgetUsd).ok;
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

/** Populates the form dollars input from an entity budget (empty when unset). */
export function entityBudgetUsdInput(entity: unknown): string {
  const cents = entityBudgetCents(entity);
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

/** Derives budget vs. spend from the session API payload; null when unbudgeted. */
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

/** Detail-row display value for a deployment/session budget. */
export function entityBudgetDisplay(entity: DeploymentApiResponse | SessionApiResponse): string {
  const cents = entityBudgetCents(entity);
  return cents === null ? '—' : formatUsdCents(cents);
}
