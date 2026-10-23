import { storeZip, type ZipEntry } from './datasetZip';

export const datasetTemplateFileName = 'cases.jsonl';
export const datasetTemplateZipName = 'dataset-template.zip';

export function datasetTemplateJSONL(locale: string) {
  const chinese = locale.toLowerCase().startsWith('zh');
  const sample = chinese
    ? {
        name: '状态更新',
        turns: ['写一段本周回顾用的简短状态更新。'],
        desc: '示例用例，可以改。',
        checkpoints: ['期望任务正常结束且不报错', '最终回复包含结论', '生成一份 PDF 报告'],
        input_files: ['notes.txt'],
        reference_files: ['answer.txt'],
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
        input_files: ['notes.txt'],
        reference_files: ['answer.txt'],
      };
  return `${JSON.stringify(sample)}\n`;
}

export function datasetTemplateEntries(locale: string): ZipEntry[] {
  const encoder = new TextEncoder();
  const chinese = locale.toLowerCase().startsWith('zh');
  const notes = chinese ? '给被测 Agent 的示例输入。\n' : 'Sample input for the agent under test.\n';
  const answer = chinese ? '只给评估器看的示例参考。\n' : 'Sample reference for the evaluator only.\n';
  return [
    { name: datasetTemplateFileName, data: encoder.encode(datasetTemplateJSONL(locale)) },
    { name: 'input_files/notes.txt', data: encoder.encode(notes) },
    { name: 'reference_files/answer.txt', data: encoder.encode(answer) },
  ];
}

export function datasetTemplateZip(locale: string) {
  return storeZip(datasetTemplateEntries(locale));
}

export function downloadDatasetTemplate(locale: string) {
  const bytes = datasetTemplateZip(locale);
  const blob = new Blob([Uint8Array.from(bytes)], { type: 'application/zip' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = datasetTemplateZipName;
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
