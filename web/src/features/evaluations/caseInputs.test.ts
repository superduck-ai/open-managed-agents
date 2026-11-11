import { expect, test } from 'bun:test';
import { blockTypeForMime, buildInputs, fileBlocks, mediaCounts, textFromInputs } from './caseInputs';

test('case inputs keep text and workspace files', () => {
  const inputs = buildInputs('描述这张图', [{ id: 'file_1', kind: 'image' }]);
  expect(textFromInputs(inputs)).toBe('描述这张图');
  expect(fileBlocks(inputs)).toEqual([{ id: 'file_1', kind: 'image' }]);
  expect(mediaCounts(inputs)).toEqual({ images: 1, documents: 0 });
  expect(buildInputs('  ', [{ id: 'file_2', kind: 'document' }])).toEqual([
    { type: 'document', source: { type: 'file', file_id: 'file_2' } },
  ]);
  expect(blockTypeForMime('image/png')).toBe('image');
  expect(blockTypeForMime('application/pdf')).toBe('document');
  expect(blockTypeForMime('image/bmp')).toBeNull();
});
