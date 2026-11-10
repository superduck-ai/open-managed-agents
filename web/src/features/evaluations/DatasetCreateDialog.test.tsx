import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { DatasetCreateDialog, datasetCreateName } from './DatasetCreateDialog';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

test('a blank generated dataset uses the localized name instead of a fixed Chinese title', () => {
  expect(datasetCreateName('generate', '  ', '', 'My dataset')).toBe('My dataset');
  expect(datasetCreateName('generate', '销售回归', '', 'My dataset')).toBe('销售回归');
  expect(datasetCreateName('upload', '', 'cases.csv', 'My dataset')).toBe('cases.csv');
});

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
  expect(screen.getByText('支持 JSON、JSONL、CSV 和 Excel（xlsx）。只有 xlsx 能放图片。')).toBeTruthy();
  expect(document.querySelector('input[type="file"]')?.getAttribute('accept')).toBe(
    '.json,.jsonl,.csv,.xlsx,application/json,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  );
  expect(screen.getByText('样例文件')).toBeTruthy();
  expect(screen.getByText('只有 Excel（xlsx）能在单元格里放图片。CSV、JSON、JSONL 只能写文字。')).toBeTruthy();
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
  fireEvent.click(screen.getByRole('button', { name: 'Excel 样例（含图片）' }));
  expect(downloads).toEqual(['evaluation-dataset.sample.json', 'evaluation-dataset.sample.xlsx']);

  fireEvent.click(screen.getByRole('tab', { name: '粘贴 JSON' }));
  expect(
    screen.getByText(
      '每条都要填写用户问题。也可以补充期望回答、希望调用的工具。粘贴不能直接放新图片。图片或文档如果已经在文件库里，可以在 inputs 里引用。新图片请放到 xlsx 单元格里再上传。',
    ),
  ).toBeTruthy();
});
