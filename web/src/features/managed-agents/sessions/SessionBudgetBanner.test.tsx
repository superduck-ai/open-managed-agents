import { afterEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { cleanup, fireEvent, mockManagedResourceApi, resetTestDom, screen } from '../ManagedAgentsPage.test-utils';
import { I18nProvider } from '../../../shared/i18n';
import { budgetWireBody, sessionBudgetState } from '../resources/budget';
import { SessionBudgetBanner } from './SessionBudgetBanner';
import { useSessionBudgetSync } from './useSessionBudgetSync';
import { type SessionApiResponse } from '../types';
const { render } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('中文预算提示和编辑弹窗使用翻译及本地化金额', async () => {
  resetTestDom();
  render(
    <I18nProvider initialLocale="zh-CN">
      <SessionBudgetBanner
        state={{ budgetCents: 1, spentCents: 7, reached: true }}
        busy={false}
        onChangeBudget={async () => undefined}
      />
    </I18nProvider>,
  );
  expect(screen.getByText(/已达到预算上限/).textContent).toContain('US$0.07');
  expect(screen.queryByText(/Budget reached/)).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '调整预算' }));
  expect(await screen.findByRole('dialog')).toBeTruthy();
  expect(screen.getByText('请设置高于已使用金额的美元预算上限，以继续发送消息。')).toBeTruthy();
});

test('英文预算金额占位符正确插值', () => {
  resetTestDom();
  render(
    <I18nProvider initialLocale="en">
      <SessionBudgetBanner
        state={{ budgetCents: 100, spentCents: 1, reached: false }}
        busy={false}
        onChangeBudget={async () => undefined}
      />
    </I18nProvider>,
  );
  expect(screen.getByText('Budget $1.00 · spent $0.01')).toBeTruthy();
});

function BudgetSyncProbe({ initial, rejected }: { initial: SessionApiResponse; rejected: boolean }) {
  const [session, setSession] = useState<SessionApiResponse | null>(initial);
  useSessionBudgetSync(
    session,
    rejected ? [] : [{ id: 'usage_end', type: 'session.usage' }],
    'default',
    rejected ? 'session budget reached: only settlement events are accepted' : null,
    setSession,
  );
  const state = session && sessionBudgetState(session);
  return state ? <SessionBudgetBanner state={state} busy={false} onChangeBudget={async () => undefined} /> : null;
}

for (const rejected of [false, true]) {
  test(`预算用量陈旧时通过${rejected ? '后端拒绝' : '用量事件'}刷新并显示统一警告`, async () => {
    resetTestDom();
    const api = mockManagedResourceApi();
    const server = api.resources.sessions[0];
    Object.assign(server, { budget: budgetWireBody(1), usage: { list_cost: { amount: '7', currency: 'USD' } } });
    const initial = {
      ...server,
      usage: { list_cost: { amount: '0', currency: 'USD' } },
    } as unknown as SessionApiResponse;
    render(
      <I18nProvider initialLocale="zh-CN">
        <BudgetSyncProbe initial={initial} rejected={rejected} />
      </I18nProvider>,
    );
    expect(await screen.findByText(/已达到预算上限/)).toBeTruthy();
    expect(screen.getByRole('button', { name: '移除预算' })).toBeTruthy();
    expect(screen.queryByText(/已使用 US\$0.00/)).toBeNull();
  });
}
