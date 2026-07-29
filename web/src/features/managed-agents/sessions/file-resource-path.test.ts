import { describe, expect, test } from 'bun:test';
import { isValidSessionFileMountPath, sessionFileAPIMountPath, sessionFileRuntimePath } from './file-resource-path';

describe('session file resource paths', () => {
  test('rejects paths outside uploads and ambiguous path segments', () => {
    for (const path of [
      '/workspace/input.csv',
      '/uploads',
      '/uploads/input.csv/',
      '/uploads/reports//input.csv',
      '/uploads/./input.csv',
      '/uploads/reports/../input.csv',
    ]) {
      expect(isValidSessionFileMountPath(path)).toBe(false);
    }
  });

  test('returns safe fallback values for invalid paths', () => {
    expect(sessionFileRuntimePath('/uploads/../secret.txt')).toBe('');
    expect(sessionFileAPIMountPath(' relative.txt ')).toBe('relative.txt');
  });

  test('validates and translates uploads paths', () => {
    const path = ' /uploads/reports/input.csv ';

    expect(isValidSessionFileMountPath(path)).toBe(true);
    expect(sessionFileRuntimePath(path)).toBe('/mnt/session/uploads/reports/input.csv');
    expect(sessionFileAPIMountPath(path)).toBe('/reports/input.csv');
  });
});
