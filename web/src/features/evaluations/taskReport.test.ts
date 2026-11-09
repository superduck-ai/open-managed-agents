import { expect, test } from 'bun:test';

import { averageDimensions, failedCount, filterEvaluationTasks } from './taskReport';

test('failed count is the scored cases that did not pass', () => {
  expect(failedCount(undefined)).toBe(0);
  expect(failedCount({ scored: 4, passed: 1 })).toBe(3);
});

test('dimension averages ignore missing names', () => {
  expect(
    averageDimensions([
      { score: { dimensions: [{ name: '正确性', score: 80 }, { score: 10 }] } },
      { score: { dimensions: [{ name: '正确性', score: 40 }] } },
    ] as never),
  ).toEqual([{ name: '正确性', score: 60 }]);
});

test('task filters combine keyword and status', () => {
  const tasks = [
    { name: '退款', agent_id: 'agent_a', status: 'completed' },
    { name: '退货', agent_id: 'agent_b', status: 'running' },
  ];
  expect(filterEvaluationTasks(tasks, 'agent_b', 'all')).toEqual([tasks[1]]);
  expect(filterEvaluationTasks(tasks, '', 'running')).toEqual([tasks[1]]);
  expect(filterEvaluationTasks(tasks, '退款', 'running')).toEqual([]);
});
