import { expect, test } from 'bun:test';
import { datasetWithUploadedFiles, fileNamesInDataset, missingCaseFileNames } from './caseFileRefs';

test('uploaded spreadsheet pictures replace file names', () => {
  const content = '[{"query":"描述这张图里的金额","files":["chart.png"]}]';
  expect(fileNamesInDataset(content)).toEqual(['chart.png']);
  expect(missingCaseFileNames(content, [])).toEqual(['chart.png']);
  const json = datasetWithUploadedFiles(content, { 'chart.png': { id: 'file_chart', kind: 'image' } });
  const cases = JSON.parse(json) as Array<{ inputs?: Array<{ source?: { file_id?: string } }> }>;
  expect(cases[0]?.inputs?.[1]?.source?.file_id).toBe('file_chart');
  expect(json).not.toContain('"files"');
});

test('jsonl lines name their own files', () => {
  const content = '{"query":"描述这张图","files":["chart.png"]}\n{"query":"下一题"}\n';
  expect(fileNamesInDataset(content)).toEqual(['chart.png']);
});
