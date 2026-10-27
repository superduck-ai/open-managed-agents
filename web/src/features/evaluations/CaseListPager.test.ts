import { expect, test } from 'bun:test';

import { casePageIndexes, casePageSlice } from './CaseListPager';

test('case pages keep twenty rows and show the first five numbers', () => {
  const rows = Array.from({ length: 100 }, (_, index) => index);
  const first = casePageSlice(rows, 0);
  expect(first).toEqual({ page: 0, pageCount: 5, items: rows.slice(0, 20) });
  expect(casePageSlice(rows, 9).page).toBe(4);
  expect(casePageIndexes(0, 5)).toEqual([0, 1, 2, 3, 4]);
  expect(casePageIndexes(0, 10)).toEqual([0, 1, 2, 3, 4, 'gap', 9]);
  expect(casePageIndexes(5, 10)).toEqual([0, 'gap', 4, 5, 6, 'gap', 9]);
});
