import { expect, test } from 'bun:test';
import { advanceEvaluationCursor } from './useEvaluationCursor';

test('the next dataset page token is stored at the page being opened', () => {
  const first = advanceEvaluationCursor({ index: 0, tokens: [] }, 'page-2');
  expect(first.index).toBe(1);
  expect(first.tokens[1]).toBe('page-2');

  const second = advanceEvaluationCursor(first, 'page-3');
  expect(second.index).toBe(2);
  expect(second.tokens[2]).toBe('page-3');
  expect(second.tokens[1]).toBe('page-2');
});
