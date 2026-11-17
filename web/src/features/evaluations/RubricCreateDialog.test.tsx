import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { RubricCreateDialog } from './RubricCreateDialog';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

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

test('rubric dialog switches between generation and a written template', () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider initialLocale="zh-CN">
        <RubricCreateDialog open onOpenChange={() => undefined} />
      </I18nProvider>
    </QueryClientProvider>,
  );

  expect(screen.getByText('生成依据')).toBeTruthy();
  expect(screen.getByRole('button', { name: '生成评分标准' })).toBeTruthy();

  fireEvent.click(screen.getByRole('tab', { name: '手动编写' }));

  expect(screen.getByText('评分规则')).toBeTruthy();
  expect(screen.getByPlaceholderText(/# 评分标准/)).toBeTruthy();
  expect(screen.getByRole('button', { name: '创建' })).toBeTruthy();
});

test('a generated rubric is an editable draft until it is created', async () => {
  const requests: Array<{ url: string; body: unknown }> = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    requests.push({ url, body: init?.body ? JSON.parse(String(init.body)) : null });
    if (url.endsWith('/rubrics/generate')) {
      return Response.json({
        type: 'evaluation_rubric_draft',
        body: '## 评分维度\n\n### 准确性（100 分）',
        pass_score: 60,
      });
    }
    if (url.endsWith('/rubrics') && init?.method === 'POST') return Response.json({ id: 'evalrb_1' });
    return Response.json({ data: [], next_page: null });
  }) as typeof fetch;
  let open = true;
  try {
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <WorkspaceContext.Provider value={scope}>
          <I18nProvider initialLocale="zh-CN">
            <RubricCreateDialog open onOpenChange={(next) => (open = next)} />
          </I18nProvider>
        </WorkspaceContext.Provider>
      </QueryClientProvider>,
    );

    fireEvent.change(document.querySelector('#evaluation-rubric-goal') as HTMLInputElement, {
      target: { value: '回答准确' },
    });
    fireEvent.click(screen.getByRole('button', { name: '生成评分标准' }));
    const editor = (await screen.findByDisplayValue(/## 评分维度/)) as HTMLTextAreaElement;
    expect(screen.getByText('这是生成的草稿，确认或修改后再创建。')).toBeTruthy();
    expect(requests.some((request) => request.url.endsWith('/rubrics') && request.body)).toBe(false);

    fireEvent.change(editor, { target: { value: '## 评分维度\n\n### 准确性（100 分）\n- 是否给出订单号' } });
    fireEvent.click(screen.getByRole('button', { name: '创建' }));
    await waitFor(() => expect(open).toBe(false));
    const created = requests.find((request) => request.url.endsWith('/rubrics'));
    expect(created?.body).toEqual({
      name: '评分标准',
      desc: '',
      body: '## 评分维度\n\n### 准确性（100 分）\n- 是否给出订单号',
      pass_score: 60,
    });
  } finally {
    globalThis.fetch = originalFetch;
  }
});
