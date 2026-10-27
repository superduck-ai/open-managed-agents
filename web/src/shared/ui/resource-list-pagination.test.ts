import { describe, expect, test } from 'bun:test';
import { resourceListPageCount } from './resource-list-pagination';

describe('resourceListPageCount', () => {
  test('returns null when the total is missing or invalid', () => {
    expect(resourceListPageCount(undefined, 10)).toBeNull();
    expect(resourceListPageCount(null, 10)).toBeNull();
    expect(resourceListPageCount(Number.NaN, 10)).toBeNull();
    expect(resourceListPageCount(-1, 10)).toBeNull();
    expect(resourceListPageCount(10, 0)).toBeNull();
  });

  test('counts pages from the shared list size', () => {
    expect(resourceListPageCount(0, 10)).toBe(1);
    expect(resourceListPageCount(1, 10)).toBe(1);
    expect(resourceListPageCount(10, 10)).toBe(1);
    expect(resourceListPageCount(11, 10)).toBe(2);
    expect(resourceListPageCount(20, 10)).toBe(2);
  });
});
