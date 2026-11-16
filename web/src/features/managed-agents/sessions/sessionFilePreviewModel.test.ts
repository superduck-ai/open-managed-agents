import { expect, test } from 'bun:test';
import {
  loadSessionFilePreview,
  parseDelimitedText,
  SESSION_FILE_PREVIEW_COLUMN_LIMIT,
  SESSION_FILE_PREVIEW_ROW_LIMIT,
  sessionFilePreviewKind,
} from './sessionFilePreviewModel';

const MB = 1024 * 1024;

function candidate(filename: string, sizeBytes = 100, downloadable = true) {
  return { filename, size_bytes: sizeBytes, downloadable };
}

test('refuses previews for unsupported, oversized, extensionless and non-downloadable files', () => {
  expect(sessionFilePreviewKind(candidate('deck.pptx'))).toBeNull();
  expect(sessionFilePreviewKind(candidate('legacy.xls'))).toBeNull();
  expect(sessionFilePreviewKind(candidate('README'))).toBeNull();
  expect(sessionFilePreviewKind(candidate('constructor'))).toBeNull();
  expect(sessionFilePreviewKind(candidate('notes.md', MB + 1))).toBeNull();
  expect(sessionFilePreviewKind(candidate('page.html', 5 * MB + 1))).toBeNull();
  expect(sessionFilePreviewKind(candidate('notes.md', 100, false))).toBeNull();
});

test('chooses the renderer from the file extension regardless of case', () => {
  expect(
    ['a.py', 'a.MD', 'a.csv', 'a.tsv', 'a.xlsx', 'a.docx', 'a.HTML', 'a.pdf', 'a.svg', 'a.tar.json'].map((filename) =>
      sessionFilePreviewKind(candidate(filename)),
    ),
  ).toEqual(['code', 'markdown', 'csv', 'csv', 'xlsx', 'docx', 'html', 'pdf', 'image', 'code']);
  expect(sessionFilePreviewKind(candidate('report.pdf', 20 * MB))).toBe('pdf');
});

test('parses quoted cells, escaped quotes, embedded newlines, CRLF and a leading byte order mark', () => {
  expect(parseDelimitedText('﻿name,note\r\n"Doe, Jane","said ""hi""\nagain"\nlast,', ',')).toEqual([
    ['name', 'note'],
    ['Doe, Jane', 'said "hi"\nagain'],
    ['last', ''],
  ]);
  expect(parseDelimitedText('a\tb\n', '\t')).toEqual([['a', 'b']]);
  expect(parseDelimitedText('', ',')).toEqual([]);
});

test('caps table previews and reports the full row count', async () => {
  const text = Array.from({ length: SESSION_FILE_PREVIEW_ROW_LIMIT + 5 }, (_, index) => `row${index},x`).join('\n');
  const content = await loadSessionFilePreview('csv', new Blob([text]), 'big.csv');
  if (content.kind !== 'table') throw new Error('expected a table preview');
  expect(content.sheets[0].rows).toHaveLength(SESSION_FILE_PREVIEW_ROW_LIMIT);
  expect(content.sheets[0].totalRows).toBe(SESSION_FILE_PREVIEW_ROW_LIMIT + 5);
});

test('caps table columns and reports the full column count', async () => {
  const wide = Array.from({ length: SESSION_FILE_PREVIEW_COLUMN_LIMIT + 3 }, (_, index) => `c${index}`).join(',');
  const text = `a,b\n${'1,2\n'.repeat(SESSION_FILE_PREVIEW_ROW_LIMIT)}${wide}`;
  const content = await loadSessionFilePreview('csv', new Blob([text]), 'wide.csv');
  if (content.kind !== 'table') throw new Error('expected a table preview');
  expect(content.sheets[0].rows.every((row) => row.length === 2)).toBe(true);
  expect(content.sheets[0].totalColumns).toBe(SESSION_FILE_PREVIEW_COLUMN_LIMIT + 3);
});

test('forces the blob type from the extension and only lets HTML frames run scripts', async () => {
  const disguised = new Blob(['<script>alert(1)</script>'], { type: 'text/html' });
  const pdf = await loadSessionFilePreview('pdf', disguised, 'report.pdf');
  if (pdf.kind !== 'frame') throw new Error('expected a frame preview');
  expect(pdf.blob.type).toBe('application/pdf');
  expect(pdf.sandbox).toBeUndefined();
  const image = await loadSessionFilePreview('image', disguised, 'chart.SVG');
  expect(image.kind === 'image' && image.blob.type).toBe('image/svg+xml');
  const html = await loadSessionFilePreview('html', new Blob(['<p>hi</p>'], { type: 'application/pdf' }), 'page.html');
  if (html.kind !== 'frame') throw new Error('expected a frame preview');
  expect(html.blob.type).toStartWith('text/html');
  expect(html.sandbox).toBe('allow-scripts');
  expect(await html.blob.text()).toBe('<p>hi</p>');
  expect(await loadSessionFilePreview('code', new Blob(['print(1)']), 'main.py')).toEqual({
    kind: 'code',
    text: 'print(1)',
    language: 'py',
  });
});
