import { expect, test } from 'bun:test';

import { acceptanceItems, joinAcceptance } from './caseChecklist';

test('acceptance checklist keeps blank rows editable and stores one item per line', () => {
  expect(acceptanceItems('')).toEqual(['']);
  expect(acceptanceItems(' 首句给结论 \n\n 数字可复算 ')).toEqual(['首句给结论', '数字可复算']);
  expect(joinAcceptance(['首句给结论', ' ', '数字可复算'])).toBe('首句给结论\n数字可复算');
});
