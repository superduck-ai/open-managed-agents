import { expect, test } from 'bun:test';
import { sessionOutputFileForPath, sessionOutputFilePaths } from './sessionOutputModel';

const metadata = (id: string) => ({
  id,
  filename: '笑话.txt',
  type: 'file' as const,
  mime_type: 'text/plain',
  size_bytes: 12,
  downloadable: true,
  created_at: '2026-10-15T00:00:00Z',
});

test('never guesses a file path from a basename or confuses same-name files across directories', () => {
  const files = sessionOutputFilePaths(
    [metadata('a'), metadata('b'), metadata('unmapped')],
    [
      { id: 'ra', type: 'file', file_id: 'a', mount_path: '/outputs/one/笑话.txt' },
      { id: 'rb', type: 'file', file_id: 'b', mount_path: '/outputs/two/笑话.txt' },
    ],
  );
  expect(files[2].path).toBeUndefined();
  expect(sessionOutputFileForPath(files, '/outputs/笑话.txt')).toBeUndefined();
  expect(sessionOutputFileForPath(files, 'https://example.com/outputs/one/笑话.txt')).toBeUndefined();
  expect(sessionOutputFileForPath(files, '/outputs/%invalid')).toBeUndefined();
  expect(sessionOutputFileForPath(files, 'sandbox:/mnt/user-data/outputs/one/%E7%AC%91%E8%AF%9D.txt')?.id).toBe('a');
  expect(sessionOutputFileForPath(files, '/outputs/two/笑话.txt')?.id).toBe('b');
});

test('decodes hrefs once while retaining literal percent characters in filesystem paths', () => {
  const files = [
    { ...metadata('percent'), path: '/outputs/a%20b.txt' },
    { ...metadata('space'), path: '/outputs/a b.txt' },
  ];
  expect(sessionOutputFileForPath(files, '/outputs/a%2520b.txt')?.id).toBe('percent');
  expect(sessionOutputFileForPath(files, '/outputs/a%20b.txt')?.id).toBe('space');
});
