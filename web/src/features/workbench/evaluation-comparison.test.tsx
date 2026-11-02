import { afterEach, expect, mock, test } from 'bun:test';
import { useState } from 'react';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { resetTestDom } from '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { EvaluateView } from './evaluate';
import type { EvaluateComparison, EvaluateTestCase } from './model';

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function EvaluationHarness({ orgUuid = 'org_one', promptId = 'prompt_one' }: { orgUuid?: string; promptId?: string }) {
  const [rows, setRows] = useState<EvaluateTestCase[]>([]);
  const [comparisons, setComparisons] = useState<EvaluateComparison[]>([]);
  return (
    <I18nProvider initialLocale="zh-CN">
      <EvaluateView
        variables={['topic']}
        promptTitle="订单摘要"
        promptText="整理 {{topic}}。"
        rows={rows}
        setRows={setRows}
        comparisons={comparisons}
        setComparisons={setComparisons}
        orgUuid={orgUuid}
        promptId={promptId}
        currentRevisionId="revision_current"
        fallbackModelName="sample-model"
        isReadOnly={false}
        saveRevisionLabel="v3"
        canSaveRevision={false}
        onSaveRevision={() => undefined}
        onCreateRow={async () => undefined}
        onUpdateVariables={async () => undefined}
        onUpdateIdealOutput={async () => undefined}
        onDeleteRow={async () => undefined}
        onGenerateTestCases={async () => undefined}
      />
    </I18nProvider>
  );
}

function revisionList(createdAt = '2026-10-25T00:00:00Z') {
  const revision = {
    id: 'revision_current',
    created_at: createdAt,
    is_latest: true,
    model_name: 'sample-model',
    system_prompt: '',
    messages: [{ role: 'human', content: [{ type: 'text', text: '整理 {{topic}}。' }] }],
    variables: ['topic'],
    tools: [],
    max_tokens_to_sample: 1024,
    thinking: { type: 'disabled' },
    show_raw_thinking: false,
    skip_system_modification: false,
  };
  return [revision, { ...revision, id: 'revision_previous', is_latest: false }];
}

function response(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), { status, headers: { 'Content-Type': 'application/json' } });
}

test('distinguishes comparison loading and failure from an empty list and retries inside the menu', async () => {
  resetTestDom('https://oma.duck.ai/workbench');
  let release!: (value: Response) => void;
  const pending = new Promise<Response>((resolve) => {
    release = resolve;
  });
  let attempts = 0;
  globalThis.fetch = mock(async (input) => {
    expect(String(input)).toContain('/workbench/prompts/prompt_one/revisions');
    attempts += 1;
    return attempts === 1 ? pending : response(revisionList());
  });
  render(<EvaluationHarness />);
  fireEvent.click(screen.getByRole('button', { name: '添加对照' }));
  expect(await screen.findByRole('status')).toBeTruthy();
  expect(screen.queryByText('暂无可对照的版本')).toBeNull();
  await act(async () => release(response({ error: { message: 'Sample failure' } }, 503)));
  expect((await screen.findByRole('alert')).textContent).toContain('版本列表加载失败');
  expect(screen.queryByText('暂无可对照的版本')).toBeNull();
  fireEvent.click(screen.getByRole('menuitem', { name: '重试' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: /版本 1/ }));
  expect(screen.getByRole('button', { name: '移除 v1 对照' })).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
  expect(attempts).toBe(2);
});

test('shows an empty comparison list only after a successful empty response', async () => {
  resetTestDom('https://oma.duck.ai/workbench');
  globalThis.fetch = mock(async () => response([]));
  render(<EvaluationHarness />);
  fireEvent.click(screen.getByRole('button', { name: '添加对照' }));
  expect(await screen.findByText('暂无可对照的版本')).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.queryByRole('menuitem', { name: '重试' })).toBeNull();
});

test.each([false, true])(
  'ignores a previous scope response after switching organization and prompt (failure=%s)',
  async (failed) => {
    resetTestDom('https://oma.duck.ai/workbench');
    let release!: (value: Response) => void;
    const pending = new Promise<Response>((resolve) => {
      release = resolve;
    });
    globalThis.fetch = mock(async (input) =>
      String(input).includes('prompt_one') ? pending : response(revisionList()),
    );
    const view = render(<EvaluationHarness />);
    fireEvent.click(screen.getByRole('button', { name: '添加对照' }));
    expect(await screen.findByRole('status')).toBeTruthy();
    view.rerender(<EvaluationHarness orgUuid="org_two" promptId="prompt_two" />);
    fireEvent.click(screen.getByRole('button', { name: '添加对照' }));
    expect(await screen.findByRole('menuitem', { name: /版本 1/ })).toBeTruthy();
    await act(async () =>
      release(
        failed
          ? response({ error: { message: 'Old scope failed' } }, 503)
          : response(revisionList('2026-06-12T00:00:00Z')),
      ),
    );
    expect(screen.getByRole('menuitem', { name: /10月25日/ })).toBeTruthy();
    expect(screen.queryByRole('menuitem', { name: /6月12日/ })).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
  },
);

test('labels the baseline as the current version without inventing v1 or duplicating the older comparison label', async () => {
  resetTestDom('https://oma.duck.ai/workbench');
  globalThis.fetch = mock(async () => response(revisionList()));
  render(<EvaluationHarness />);
  expect(screen.getByText('当前版本')).toBeTruthy();
  expect(screen.queryByText('v1', { exact: true })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '添加对照' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: /版本 1/ }));
  expect(screen.getByText('当前版本')).toBeTruthy();
  expect(screen.getAllByText('v1', { exact: true })).toHaveLength(1);
  expect(screen.getByRole('button', { name: '移除 v1 对照' })).toBeTruthy();
});
