import { expect, test } from 'bun:test';
import { compressedSampleWorkbook, readUploadedDataset, readXlsxDataset, sampleWorkbook } from './spreadsheetDataset';

test('xlsx import reads the picture stored in the cell', async () => {
  const parsed = await readXlsxDataset(sampleWorkbook().buffer);
  const cases = JSON.parse(parsed.content) as Array<{ query: string; expected_answer?: string; files?: string[] }>;
  expect(cases).toHaveLength(1);
  expect(cases[0]?.query).toBe('描述这张图里的金额');
  expect(cases[0]?.expected_answer).toBe('列出图中的合计金额');
  expect(cases[0]?.files).toEqual(['chart.png']);
  expect(parsed.images[0]?.name).toBe('chart.png');
  expect(parsed.images[0]?.bytes[0]).toBe(0x89);
  expect(parsed.content).not.toContain('"polarity"');
});

test('xlsx import reads a picture from a compressed workbook', async () => {
  const parsed = await readXlsxDataset((await compressedSampleWorkbook()).buffer);
  const cases = JSON.parse(parsed.content) as Array<{ files?: string[] }>;
  expect(cases[0]?.files).toEqual(['chart.png']);
  expect(parsed.images[0]?.bytes[0]).toBe(0x89);
});

test('an xlsx filename is not treated as the older xls format', async () => {
  const parsed = await readUploadedDataset(new File([sampleWorkbook()], 'cases.xlsx'));
  expect(parsed.error).toBeUndefined();
  expect(parsed.images[0]?.name).toBe('chart.png');
});

test('a csv export of a cell picture is rejected because it has no image bytes', async () => {
  const file = new File(['query\n=DISPIMG("ID_45",1)\n'], 'cases.csv', { type: 'text/csv' });
  const parsed = await readUploadedDataset(file);
  expect(parsed.error).toBe('csv-image');
  expect(parsed.images).toEqual([]);
});
