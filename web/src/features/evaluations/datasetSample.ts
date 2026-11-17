export type DatasetSampleKind = 'json' | 'jsonl' | 'csv';

type SampleCase = {
  query: string;
  expected_answer: string;
  expected_tool_trace?: string[];
  polarity?: 'positive' | 'negative';
  acceptance?: string;
};

const sampleFileNames: Record<DatasetSampleKind, string> = {
  json: 'evaluation-dataset.sample.json',
  jsonl: 'evaluation-dataset.sample.jsonl',
  csv: 'evaluation-dataset.sample.csv',
};

export function datasetSampleFileName(kind: DatasetSampleKind) {
  return sampleFileNames[kind];
}

export function datasetSampleCases(chinese: boolean): SampleCase[] {
  if (chinese) {
    return [
      {
        query: '汇总本月销售额',
        expected_answer: '按渠道列出总额',
        expected_tool_trace: ['lookup_sales'],
        polarity: 'positive',
        acceptance: '回答包含各渠道金额和合计',
      },
      {
        query: '没有数据时怎么回复',
        expected_answer: '说明数据缺失，不要编造数字',
        polarity: 'negative',
        acceptance: '明确说出无法计算，且不给出虚假金额',
      },
    ];
  }
  return [
    {
      query: 'Summarize this month’s sales',
      expected_answer: 'List the total for each channel',
      expected_tool_trace: ['lookup_sales'],
      polarity: 'positive',
      acceptance: 'The reply includes each channel amount and the total',
    },
    {
      query: 'What should you say when the data is missing?',
      expected_answer: 'Say the data is missing. Do not invent numbers.',
      polarity: 'negative',
      acceptance: 'The reply says the total cannot be calculated and gives no made-up amount',
    },
  ];
}

export function datasetSampleText(kind: DatasetSampleKind, chinese: boolean) {
  const cases = datasetSampleCases(chinese);
  if (kind === 'json') return `${JSON.stringify(cases, null, 2)}\n`;
  if (kind === 'jsonl') return `${cases.map((item) => JSON.stringify(item)).join('\n')}\n`;
  const header = ['query', 'expected_answer', 'expected_tool_trace', 'polarity', 'acceptance'];
  const rows = cases.map((item) =>
    [
      item.query,
      item.expected_answer,
      item.expected_tool_trace ? JSON.stringify(item.expected_tool_trace) : '',
      item.polarity ?? '',
      item.acceptance ?? '',
    ]
      .map(csvCell)
      .join(','),
  );
  return `${header.join(',')}\n${rows.join('\n')}\n`;
}

export function downloadDatasetSample(kind: DatasetSampleKind, chinese: boolean) {
  const text = kind === 'csv' ? `\uFEFF${datasetSampleText(kind, chinese)}` : datasetSampleText(kind, chinese);
  const type = kind === 'csv' ? 'text/csv;charset=utf-8' : 'application/json';
  downloadBlob(new Blob([text], { type }), datasetSampleFileName(kind));
}

export function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export function csvCell(value: string) {
  const text = /^[=+\-@\t\r]/.test(value) ? `'${value}` : value;
  if (/[",\n\r]/.test(text)) return `"${text.replaceAll('"', '""')}"`;
  return text;
}
