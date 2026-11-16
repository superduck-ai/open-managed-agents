import { expect, test } from 'bun:test';
import {
  blockTypeForMime,
  buildInputs,
  editedCaseInputs,
  fileBlocks,
  mediaCounts,
  textFromInputs,
  type EvaluationContentBlock,
} from './caseInputs';

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

const interleaved: EvaluationContentBlock[] = [
  { type: 'text', text: '文字 A' },
  { type: 'image', source: { type: 'file', file_id: 'file_img' } },
  { type: 'text', text: '文字 B' },
];

test('saving without query or attachment edits keeps interleaved inputs', () => {
  expect(editedCaseInputs(interleaved, textFromInputs(interleaved), fileBlocks(interleaved))).toEqual(interleaved);
});

test('editing the query replaces text blocks and leaves files where they were', () => {
  expect(editedCaseInputs(interleaved, '改过的问题', fileBlocks(interleaved))).toEqual([
    { type: 'text', text: '改过的问题' },
    { type: 'image', source: { type: 'file', file_id: 'file_img' } },
  ]);
});

test('removing one attachment and adding another keeps the remaining order', () => {
  const original: EvaluationContentBlock[] = [
    { type: 'image', source: { type: 'file', file_id: 'file_img' } },
    { type: 'text', text: '问题' },
    { type: 'document', source: { type: 'file', file_id: 'file_doc' } },
  ];
  expect(
    editedCaseInputs(original, '问题', [
      { id: 'file_img', kind: 'image' },
      { id: 'file_new', kind: 'document' },
    ]),
  ).toEqual([
    { type: 'image', source: { type: 'file', file_id: 'file_img' } },
    { type: 'text', text: '问题' },
    { type: 'document', source: { type: 'file', file_id: 'file_new' } },
  ]);
});
