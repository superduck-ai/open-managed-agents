import { expect, test } from 'bun:test';

import type { EvaluationItem } from './api';
import {
  averageDimensions,
  filterEvaluationTasks,
  filterItems,
  itemOutcome,
  itemsCsv,
  outcomeCounts,
  percentOf,
  taskConclusion,
} from './taskReport';

function item(patch: Partial<EvaluationItem>): EvaluationItem {
  return {
    id: 'evalitem_1',
    round: 1,
    case_id: 'evalcase_1',
    query: '退款多久到账',
    expected_answer: '',
    acceptance: '',
    session_id: '',
    status: 'completed',
    error_message: '',
    score: null,
    ...patch,
  };
}

function scored(passed: boolean, patch: Partial<EvaluationItem> = {}) {
  return item({
    score: {
      status: 'completed',
      score: passed ? 90 : 30,
      passed,
      explanation: '',
      answer: '',
      dimensions: [],
      error_message: '',
      judge_model: '',
      judge_prompt_version: '',
    },
    ...patch,
  });
}

test('item outcome treats session and scorer failures as system errors', () => {
  expect(itemOutcome(item({ status: 'failed' }), 'completed')).toBe('error');
  expect(
    itemOutcome(
      item({
        score: {
          status: 'failed',
          score: 0,
          passed: false,
          explanation: '',
          answer: '',
          dimensions: [],
          error_message: 'x',
          judge_model: '',
          judge_prompt_version: '',
        },
      }),
      'completed',
    ),
  ).toBe('error');
  expect(itemOutcome(item({ status: 'running' }), 'running')).toBe('running');
  expect(itemOutcome(item({ score: null }), 'running')).toBe('running');
  expect(itemOutcome(scored(true), 'completed')).toBe('passed');
  expect(itemOutcome(scored(false), 'completed')).toBe('failed');
});

test('item outcome keeps cancelled and unscored cases out of errors once the task stops', () => {
  expect(itemOutcome(item({ status: 'cancelled' }), 'cancelled')).toBe('unscored');
  expect(itemOutcome(item({ score: null }), 'cancelled')).toBe('unscored');
  expect(itemOutcome(item({ status: 'pending' }), 'cancelled')).toBe('unscored');
});

test('conclusion reports a cancelled task as stopped even with unscored cases', () => {
  const items = [scored(true), item({ status: 'cancelled' }), item({ score: null })];
  const counts = outcomeCounts(items, 'cancelled');
  expect(counts).toEqual({ passed: 1, failed: 0, error: 0, running: 0, unscored: 2 });
  expect(taskConclusion({ status: 'cancelled' }, counts)).toBe('cancelled');
  expect(itemsCsv({ status: 'cancelled', items }, ['问题'], (outcome) => outcome)).toContain(',unscored,');
});

test('conclusion reports incomplete results when any case errored', () => {
  const items = [scored(true), scored(false), item({ status: 'failed' })];
  const counts = outcomeCounts(items, 'completed');
  expect(counts).toEqual({ passed: 1, failed: 1, error: 1, running: 0, unscored: 0 });
  expect(taskConclusion({ status: 'completed' }, counts)).toBe('incomplete');
  expect(taskConclusion({ status: 'failed' }, { passed: 0, failed: 0, error: 2, running: 0, unscored: 0 })).toBe(
    'empty',
  );
  expect(taskConclusion({ status: 'running' }, counts)).toBe('running');
  expect(taskConclusion({ status: 'completed' }, { passed: 2, failed: 1, error: 0, running: 0, unscored: 0 })).toBe(
    'complete',
  );
});

test('item filter keeps only the chosen outcome', () => {
  const items = [scored(true, { id: 'a' }), scored(false, { id: 'b' }), item({ id: 'c', status: 'failed' })];
  expect(filterItems(items, 'all', 'completed').map((row) => row.id)).toEqual(['a', 'b', 'c']);
  expect(filterItems(items, 'error', 'completed').map((row) => row.id)).toEqual(['c']);
  expect(filterItems(undefined, 'passed', 'completed')).toEqual([]);
});

test('percent is zero for empty totals', () => {
  expect(percentOf(3, 0)).toBe(0);
  expect(percentOf(1, 3)).toBe(33);
});

test('csv export quotes commas, quotes, and new lines', () => {
  const text = itemsCsv(
    { status: 'completed', items: [scored(true, { query: 'a,"b"\nc', session_id: 'sesn_1' })] },
    ['问题'],
    (outcome) => outcome,
  );
  expect(text.split('\n')[0]).toBe('问题');
  expect(text).toContain('"a,""b""\nc",,90,passed,');
  expect(
    itemsCsv({ status: 'completed', items: [scored(false, { query: '=1+1' })] }, ['问题'], (outcome) => outcome),
  ).toContain("'=1+1");
  expect(text.trimEnd().endsWith('sesn_1')).toBe(true);
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
