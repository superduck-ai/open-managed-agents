import { expect, test } from 'bun:test';

import { datasetSampleFileName, datasetSampleText } from './datasetSample';

test('dataset samples keep a query and the optional scoring columns', () => {
  const json = JSON.parse(datasetSampleText('json', true)) as Array<Record<string, unknown>>;
  expect(json).toHaveLength(2);
  expect(json[0]?.query).toBe('汇总本月销售额');
  expect(json[0]?.expected_tool_trace).toEqual(['lookup_sales']);
  expect(json[1]?.polarity).toBe('negative');

  const lines = datasetSampleText('jsonl', false).trim().split('\n');
  expect(lines).toHaveLength(2);
  expect(JSON.parse(lines[0] ?? '').query).toBe('Summarize this month’s sales');

  const csv = datasetSampleText('csv', true);
  expect(csv.startsWith('query,expected_answer,expected_tool_trace,polarity,acceptance\n')).toBe(true);
  expect(csv).not.toContain('DISPIMG');
  expect(csv).toContain('"[""lookup_sales""]"');
  expect(datasetSampleFileName('csv')).toBe('evaluation-dataset.sample.csv');
});
