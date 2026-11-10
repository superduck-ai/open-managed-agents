import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { DatasetCreateDialog } from './DatasetCreateDialog';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

test('upload tab offers dataset sample files', () => {
  URL.createObjectURL = () => 'blob:evaluation-sample';
  URL.revokeObjectURL = () => undefined;
  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider initialLocale="zh-CN">
        <DatasetCreateDialog open onOpenChange={() => undefined} />
      </I18nProvider>
    </QueryClientProvider>,
  );

  fireEvent.click(screen.getByRole('tab', { name: '上传文件' }));

  expect(screen.getByText(/点击\/拖拽/)).toBeTruthy();
  expect(screen.getByText('支持 JSON、JSONL 和 CSV，必须包含 query 列。')).toBeTruthy();
  expect(document.querySelector('input[type="file"]')?.getAttribute('accept')).toBe(
    '.json,.jsonl,.csv,application/json,text/csv',
  );
  expect(screen.getByText('样例文件')).toBeTruthy();
  const downloads: string[] = [];
  const append = document.body.append.bind(document.body);
  document.body.append = ((node: Node) => {
    if (node.nodeName === 'A') downloads.push((node as HTMLAnchorElement).download);
    return append(node);
  }) as typeof document.body.append;

  fireEvent.click(screen.getByRole('button', { name: 'JSON 样例' }));
  expect(downloads).toEqual(['evaluation-dataset.sample.json']);
  expect(screen.getByRole('button', { name: 'JSONL 样例' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'CSV 样例' })).toBeTruthy();
});
