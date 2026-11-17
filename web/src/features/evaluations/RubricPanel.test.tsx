import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { RubricPanel } from './RubricPanel';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

const scope: WorkspaceContextValue = {
  orgUuid: 'org',
  workspaces: [defaultWorkspace],
  activeWorkspace: defaultWorkspace,
  activeWorkspaceId: 'ws',
  isLoading: false,
  error: null,
  selectWorkspace: () => undefined,
  createWorkspace: async () => defaultWorkspace,
  refreshWorkspaces: async () => undefined,
};

test('rubric list can open the next page', async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes('page=next')) {
      return Response.json({
        data: [rubric('evalrb_second', '第二页标准')],
        next_page: null,
      });
    }
    return Response.json({
      data: [rubric('evalrb_first', '第一页标准')],
      next_page: 'next',
    });
  }) as typeof fetch;
  try {
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <WorkspaceContext.Provider value={scope}>
          <I18nProvider initialLocale="zh-CN">
            <RubricPanel onCreate={() => undefined} />
          </I18nProvider>
        </WorkspaceContext.Provider>
      </QueryClientProvider>,
    );
    expect(await screen.findByText('第一页标准')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '下一页' }));
    expect(await screen.findByText('第二页标准')).toBeTruthy();
  } finally {
    globalThis.fetch = originalFetch;
  }
});

function rubric(id: string, name: string) {
  return {
    id,
    name,
    desc: '',
    version_id: 'erv_1',
    version: 1,
    body: '规则',
    pass_score: 60,
    model: '',
    created_at: '2026-11-03T07:29:35Z',
    updated_at: '2026-11-03T07:29:35Z',
  };
}
