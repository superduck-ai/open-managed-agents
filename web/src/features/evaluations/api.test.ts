import { expect, test } from 'bun:test';
import { evaluationBase, evaluationListPath } from './api';

test('evaluation routes stay inside the console organization and workspace', () => {
  expect(evaluationBase('org/1', 'ws 2')).toBe('/api/console/organizations/org%2F1/workspaces/ws%202/evaluations');
});

test('evaluation lists use the console page size', () => {
  expect(evaluationListPath('/evaluations/datasets')).toBe('/evaluations/datasets?limit=15');
  expect(evaluationListPath('/evaluations/datasets', 'cursor')).toBe('/evaluations/datasets?limit=15&page=cursor');
});
