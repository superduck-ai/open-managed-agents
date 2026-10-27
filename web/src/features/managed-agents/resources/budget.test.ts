import { describe, expect, it } from 'bun:test';
import type { SessionApiResponse } from '../types';
import {
  budgetCreateBody,
  budgetUpdateBody,
  budgetValid,
  budgetWireBody,
  entityBudgetDisplay,
  entityBudgetUsdInput,
  formatUsdCents,
  parseBudgetUsdInput,
  sessionBudgetState,
} from './budget';

function formValues(budgetUsd: string, budgetChanged = false) {
  return {
    name: '',
    description: '',
    agentId: '',
    environmentId: '',
    initialMessage: '',
    triggerType: '',
    cronExpression: '',
    timezone: '',
    vaultIds: [],
    memoryStoreIds: [],
    fileResources: [],
    gitResources: [],
    originalResources: [],
    resourcesChanged: false,
    budgetUsd,
    budgetChanged,
  };
}

describe('parseBudgetUsdInput', () => {
  it('treats empty input as no budget', () => {
    expect(parseBudgetUsdInput('')).toEqual({ ok: true, cents: null });
    expect(parseBudgetUsdInput('   ')).toEqual({ ok: true, cents: null });
  });

  it('parses dollars into integer cents', () => {
    expect(parseBudgetUsdInput('25')).toEqual({ ok: true, cents: 2500 });
    expect(parseBudgetUsdInput('12.5')).toEqual({ ok: true, cents: 1250 });
    expect(parseBudgetUsdInput('0.99')).toEqual({ ok: true, cents: 99 });
  });

  it('rejects invalid amounts', () => {
    expect(parseBudgetUsdInput('-5').ok).toBe(false);
    expect(parseBudgetUsdInput('1.234').ok).toBe(false);
    expect(parseBudgetUsdInput('1e3').ok).toBe(false);
    expect(parseBudgetUsdInput('abc').ok).toBe(false);
    expect(parseBudgetUsdInput('0').ok).toBe(false);
  });
});

describe('budget bodies', () => {
  it('builds the CMA wire shape', () => {
    expect(budgetWireBody(2500)).toEqual({
      type: 'limit',
      max_list_cost: { amount: '2500', currency: 'USD' },
    });
  });

  it('omits budget on create when unset and rejects invalid input', () => {
    expect(budgetCreateBody(formValues(''))).toBeUndefined();
    expect(budgetCreateBody(formValues('25'))).toEqual(budgetWireBody(2500));
    expect(budgetCreateBody(formValues('-1'))).toBeUndefined();
  });

  it('sends update budget only when changed; null clears', () => {
    expect(budgetUpdateBody(formValues('25'))).toBeUndefined();
    expect(budgetUpdateBody(formValues('25', true))).toEqual(budgetWireBody(2500));
    expect(budgetUpdateBody(formValues('', true))).toBeNull();
    expect(budgetUpdateBody(formValues('bad', true))).toBeNull();
  });

  it('blocks submit on invalid budget text', () => {
    expect(budgetValid(formValues(''))).toBe(true);
    expect(budgetValid(formValues('12.50'))).toBe(true);
    expect(budgetValid(formValues('1.234'))).toBe(false);
  });
});

describe('entity helpers', () => {
  it('reads budget back as a dollars input', () => {
    const entity = { budget: budgetWireBody(2500) };
    expect(entityBudgetUsdInput(entity)).toBe('25');
    expect(entityBudgetUsdInput({})).toBe('');
  });

  it('formats cents as dollars', () => {
    expect(formatUsdCents(2500)).toBe('$25');
    expect(formatUsdCents(2599)).toBe('$25.99');
  });

  it('derives reached state from usage spend', () => {
    const session = {
      budget: budgetWireBody(100),
      usage: { list_cost: { amount: '100', currency: 'USD' } },
    } as unknown as SessionApiResponse;
    expect(sessionBudgetState(session)).toEqual({ budgetCents: 100, spentCents: 100, reached: true });
    expect(sessionBudgetState({ budget: budgetWireBody(100) } as unknown as SessionApiResponse)).toEqual({
      budgetCents: 100,
      spentCents: 0,
      reached: false,
    });
    expect(sessionBudgetState({} as unknown as SessionApiResponse)).toBeNull();
  });

  it('renders display values for detail rows', () => {
    expect(entityBudgetDisplay({ budget: budgetWireBody(100) } as never)).toBe('$1');
    expect(entityBudgetDisplay({} as never)).toBe('—');
  });
});
