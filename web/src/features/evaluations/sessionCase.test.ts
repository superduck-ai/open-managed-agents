import { expect, test } from 'bun:test';

import { datasetTemplateJSONL } from './datasetTemplate';
import { checkpointsFieldValue, parseCheckpointsField } from './sessionCase';

test('plain checkpoint lines become a string array', () => {
  expect(parseCheckpointsField('期望的结果是不会报错，正常结束\n\n最终回复包含结论')).toEqual({
    checkpoints: ['期望的结果是不会报错，正常结束', '最终回复包含结论'],
  });
  expect(parseCheckpointsField('  \n  ')).toEqual({});
});

test('checkpoint json keeps string arrays and rejects objects', () => {
  expect(parseCheckpointsField('[" ready "]')).toEqual({ checkpoints: ['ready'] });
  expect(parseCheckpointsField('{"ok":true}')).toEqual({ error: true });
  expect(parseCheckpointsField('["ok", 1]')).toEqual({ error: true });
  expect(parseCheckpointsField('{')).toEqual({ error: true });
});

test('string checkpoint arrays display as lines', () => {
  expect(checkpointsFieldValue(['生成一份 PDF 报告', '调用过 search 工具'])).toBe(
    '生成一份 PDF 报告\n调用过 search 工具',
  );
  expect(checkpointsFieldValue(null)).toBe('');
});

test('dataset template is one jsonl case with string checkpoints', () => {
  const line = datasetTemplateJSONL('en').trim();
  const parsed = JSON.parse(line) as { checkpoints: string[]; turns: string[] };
  expect(parsed.turns.length).toBeGreaterThan(0);
  expect(parsed.checkpoints).toContain('Produce a PDF report');
  expect(datasetTemplateJSONL('zh-CN')).toContain('期望任务正常结束且不报错');
});
