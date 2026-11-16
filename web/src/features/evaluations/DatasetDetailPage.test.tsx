import { expect, test } from 'bun:test';
import { remainingTurnSelection } from './DatasetDetailPage';

test('imported session turns stay out of the next selection', () => {
  const selected = new Set([1, 2, 3]);
  expect([...remainingTurnSelection(selected, [1, 2])]).toEqual([3]);
  expect([...remainingTurnSelection(selected, [1, 2, 3])]).toEqual([]);
});
