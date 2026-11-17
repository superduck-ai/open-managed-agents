import { expect, test } from 'bun:test';
import { caseSourceLabel, remainingTurnSelection } from './DatasetDetailPage';

test('case source distinguishes generated cases from manual ones', () => {
  const labels = { session: '会话导入', generated: '自动生成', imported: '文件导入', manual: '手动创建' };
  expect(caseSourceLabel('generated', '', labels)).toBe('自动生成');
  expect(caseSourceLabel('import', '', labels)).toBe('文件导入');
  expect(caseSourceLabel('', 'sesn_1', labels)).toBe('会话导入');
  expect(caseSourceLabel('', '', labels)).toBe('手动创建');
});

test('imported session turns stay out of the next selection', () => {
  const selected = new Set([1, 2, 3]);
  expect([...remainingTurnSelection(selected, [1, 2])]).toEqual([3]);
  expect([...remainingTurnSelection(selected, [1, 2, 3])]).toEqual([]);
});
