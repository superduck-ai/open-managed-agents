import { describe, expect, test } from 'bun:test';
import { isValidSessionFileMountPath, sessionFileAPIMountPath, sessionFileRuntimePath } from './file-resource-path';

describe('session file resource paths', () => {
  test('rejects paths outside uploads and ambiguous path segments', () => {
    for (const path of [
      '',
      '/workspace/input.csv',
      '/uploads/input.csv',
      'input.csv/',
      'reports//input.csv',
      './input.csv',
      'reports/../input.csv',
    ]) {
      expect(isValidSessionFileMountPath(path)).toBe(false);
    }
  });

  test('returns safe fallback values for invalid paths', () => {
    expect(sessionFileRuntimePath('')).toBe('');
    expect(sessionFileAPIMountPath('')).toBeUndefined();
    expect(sessionFileRuntimePath('../secret.txt')).toBe('');
    expect(sessionFileAPIMountPath(' /absolute.txt ')).toBe('/absolute.txt');
  });

  test('validates and prefixes uploads-relative paths', () => {
    const path = ' reports/input.csv ';

    expect(isValidSessionFileMountPath(path)).toBe(true);
    expect(sessionFileRuntimePath(path)).toBe('/mnt/session/uploads/reports/input.csv');
    expect(sessionFileAPIMountPath(path)).toBe('/reports/input.csv');
  });
});
