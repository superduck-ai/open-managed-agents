import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { RubricDetailDialog } from './RubricDetailDialog';
import type { EvaluationRubric } from './api';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(cleanup);

const rubric: EvaluationRubric = {
  id: 'evalrb_quality',
  name: '客服回答质量标准',
  desc: '',
  version_id: 'erv_1',
  version: 1,
  body: '## 维度一\n\n- 5分：事实正确',
  pass_score: 60,
  model: '',
  created_at: '2026-11-03T07:29:35Z',
};

test('rubric details show the title, id, and scoring rules before editing', () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider initialLocale="zh-CN">
        <RubricDetailDialog rubric={rubric} onOpenChange={() => undefined} orgUuid="org" workspaceId="ws" />
      </I18nProvider>
    </QueryClientProvider>,
  );

  expect(screen.getByRole('heading', { name: '评分标准详情' })).toBeTruthy();
  expect(screen.getByRole('heading', { name: '客服回答质量标准' })).toBeTruthy();
  expect(screen.getByText('evalrb_quality')).toBeTruthy();
  expect(screen.getByText('v1')).toBeTruthy();
  expect(screen.getByRole('heading', { name: '评分规则' })).toBeTruthy();
  expect(screen.getByText(/事实正确/)).toBeTruthy();
  expect(screen.queryByRole('textbox')).toBeNull();

  fireEvent.click(screen.getByRole('button', { name: '编辑' }));
  expect(screen.getByRole('textbox', { name: '评分标准内容' })).toBeTruthy();
});

test('version history lists older rubric bodies and opens the selected one', async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    if (String(input).endsWith('/versions')) {
      return Response.json({
        data: [
          {
            id: 'erv_2',
            version: 2,
            body: '## 新版\n\n- 新规则',
            pass_score: 70,
            model: '',
            created_at: '2026-11-10T03:31:40Z',
          },
          {
            id: 'erv_1',
            version: 1,
            body: '## 旧版\n\n- 旧规则',
            pass_score: 60,
            model: '',
            created_at: '2026-11-03T07:29:35Z',
          },
        ],
        next_page: null,
      });
    }
    return new Response('{}', { status: 404 });
  }) as typeof fetch;
  try {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <I18nProvider initialLocale="zh-CN">
          <RubricDetailDialog rubric={rubric} onOpenChange={() => undefined} orgUuid="org" workspaceId="ws" />
        </I18nProvider>
      </QueryClientProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: '版本历史' }));
    await waitFor(() => expect(screen.getByRole('button', { name: /v1/ })).toBeTruthy());
    expect(screen.getByText('最新')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /v1/ }));
    expect(screen.getByText(/旧规则/)).toBeTruthy();
  } finally {
    globalThis.fetch = originalFetch;
  }
});
