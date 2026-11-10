import { expect, test } from 'bun:test';

import { moveSessionTurns, splitSessionTurns } from './SessionTurnTransfer';

const turns = [
  { index: 1, query: '第一问', answer: '第一答' },
  { index: 2, query: '第二问', answer: '' },
];

test('moving turns keeps conversation order on the import side', () => {
  const chosen = moveSessionTurns(new Set([2]), [1], true);
  expect(splitSessionTurns(turns, chosen)).toEqual({
    available: [],
    chosenTurns: turns,
  });
  expect([...moveSessionTurns(chosen, [2], false)]).toEqual([1]);
});
