import { afterEach, expect, test } from 'bun:test';
import {
  cleanup,
  mockManagedResourceApi,
  render,
  resetTestDom,
  screen,
  waitFor,
} from '../ManagedAgentsPage.test-utils';
import { I18nProvider } from '../../../shared/i18n';
import { ManagedEntityDialog } from './dialogs';
import { type SessionApiResponse } from '../types';
import { budgetWireBody } from './budget';

const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

for (const mode of ['create', 'unbudgeted', 'budgeted'] as const) {
  test(`会话预算字段遵守创建和编辑限制：${mode}`, async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions');
    mockManagedResourceApi();
    const entity: SessionApiResponse | undefined =
      mode === 'create'
        ? undefined
        : {
            id: 'sesn_budget',
            type: 'session',
            status: 'idle',
            archived_at: null,
            created_at: '2026-10-10T00:00:00Z',
            updated_at: '2026-10-10T00:00:00Z',
            resources: [],
            agent: { type: 'agent', id: 'agent_budget' },
            environment_id: 'env_budget',
            budget: mode === 'budgeted' ? budgetWireBody(100) : null,
          };
    render(
      <I18nProvider initialLocale="zh-CN">
        <ManagedEntityDialog
          section="sessions"
          title="预算验收"
          entity={entity}
          workspaceId="default"
          onClose={() => undefined}
          onSubmit={async () => undefined}
        />
      </I18nProvider>,
    );
    await waitFor(() => expect(screen.queryByText('加载中...')).toBeNull());
    if (mode === 'create' || mode === 'budgeted') {
      expect(await screen.findByRole('textbox', { name: /预算/ })).toBeTruthy();
    } else {
      expect(screen.queryByRole('textbox', { name: /预算/ })).toBeNull();
    }
  });
}
