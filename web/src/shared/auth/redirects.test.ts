import { describe, expect, test } from 'bun:test';
import { loginHrefForReturnTo, normalizeReturnTo, returnToFromSearch } from './redirects';

describe('auth redirects', () => {
  test('keeps same-origin app paths', () => {
    expect(normalizeReturnTo('/files?after=file_123#table')).toBe('/files?after=file_123#table');
  });

  test('falls back for unsafe return targets', () => {
    expect(normalizeReturnTo('https://example.com')).toBe('/');
    expect(normalizeReturnTo('//example.com')).toBe('/');
    expect(normalizeReturnTo('/login')).toBe('/');
  });

  test('reads login returnTo search params', () => {
    expect(returnToFromSearch('?returnTo=%2Fapi-keys')).toBe('/api-keys');
  });

  test('builds login hrefs with encoded return targets', () => {
    expect(loginHrefForReturnTo('/workbench?mode=test')).toBe('/login?returnTo=%2Fworkbench%3Fmode%3Dtest');
  });
});
