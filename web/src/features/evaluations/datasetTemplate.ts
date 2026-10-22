export const datasetTemplateFileName = 'cases.jsonl';

export function datasetTemplateJSONL(locale: string) {
  const chinese = locale.toLowerCase().startsWith('zh');
  const sample = chinese
    ? {
        name: '状态更新',
        turns: ['写一段本周回顾用的简短状态更新。'],
        desc: '示例用例，可以改。',
        checkpoints: ['期望任务正常结束且不报错', '最终回复包含结论', '生成一份 PDF 报告'],
      }
    : {
        name: 'status-update',
        turns: ['Write a short status update for the weekly review.'],
        desc: 'Sample case. Edit it.',
        checkpoints: [
          'The task finishes without an error',
          'The final reply includes the conclusion',
          'Produce a PDF report',
        ],
      };
  return `${JSON.stringify(sample)}\n`;
}

export function downloadDatasetTemplate(locale: string) {
  const blob = new Blob([datasetTemplateJSONL(locale)], { type: 'application/x-ndjson' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = datasetTemplateFileName;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

export const datasetZipTree = `dataset.zip
├── cases.jsonl
├── input_files/
│   └── notes.txt
└── reference_files/
    └── answer.txt`;
