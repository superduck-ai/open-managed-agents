import { expect, test } from 'bun:test';
import { evaluationBase, evaluationListPath, formatPassRate, formatScore } from './api';

test('evaluation routes stay inside the console organization and workspace', () => {
  expect(evaluationBase('org/1', 'ws 2')).toBe('/api/console/organizations/org%2F1/workspaces/ws%202/evaluations');
});

test('evaluation lists use the console page size', () => {
  expect(evaluationListPath('/evaluations/tasks')).toBe('/evaluations/tasks?limit=15');
  expect(evaluationListPath('/evaluations/tasks', 'cursor')).toBe('/evaluations/tasks?limit=15&page=cursor');
});

test('pass rate ignores missing scores and rounds the scored ratio', () => {
  expect(formatPassRate(null)).toBe('—');
  expect(formatPassRate(0.666)).toBe('67%');
  expect(formatScore(80)).toBe('80.0');
  expect(formatScore(null)).toBe('—');
});
