import { fileNamesInDataset } from './caseFileRefs';
import { blockTypeForMime } from './caseInputs';

class SpreadsheetImageError extends Error {}

export type PendingImage = {
  name: string;
  bytes: Uint8Array;
  mime: string;
  kind: 'image' | 'document';
};

type SheetCell = { column: number; text: string; formula: string };
type SheetRow = { row: number; cells: SheetCell[] };

const tinyPng = Uint8Array.from(
  atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='),
  (char) => char.charCodeAt(0),
);

export function sampleWorkbook() {
  return zipStored(workbookParts());
}

export async function compressedSampleWorkbook() {
  return zipCompressed(workbookParts());
}

function workbookParts() {
  const shared = sharedStringsXml(['query', 'expected_answer', '描述这张图里的金额', '列出图中的合计金额']);
  const sheet = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1"/><c r="C1" t="s"><v>1</v></c></row>
    <row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"/><c r="C2" t="s"><v>3</v></c><c r="D2"><f>DISPIMG("ID_SAMPLE",1)</f></c></row>
  </sheetData>
</worksheet>`;
  const cellImages = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<etc:cellImages xmlns:etc="http://www.wps.cn/officeDocument/2017/etCustomData" xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <etc:cellImage><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="2" name="ID_SAMPLE"/></xdr:nvPicPr><xdr:blipFill><a:blip r:embed="rId1"/></xdr:blipFill></xdr:pic></etc:cellImage>
</etc:cellImages>`;
  return {
    '[Content_Types].xml': xml(
      '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/sharedStrings.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sharedStrings+xml"/></Types>',
    ),
    '_rels/.rels': xml(
      '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>',
    ),
    'xl/workbook.xml': xml(
      '<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>',
    ),
    'xl/_rels/workbook.xml.rels': xml(
      '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>',
    ),
    'xl/sharedStrings.xml': xml(shared),
    'xl/worksheets/sheet1.xml': xml(sheet),
    'xl/cellimages.xml': xml(cellImages),
    'xl/_rels/cellimages.xml.rels': xml(
      '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/chart.png"/></Relationships>',
    ),
    'xl/media/chart.png': tinyPng,
  };
}

export async function readUploadedDataset(file: File): Promise<{
  content: string;
  images: PendingImage[];
  error?: 'xls' | 'csv-image' | 'read';
}> {
  const lower = file.name.toLowerCase();
  if (lower.endsWith('.xls')) return { content: '', images: [], error: 'xls' };
  try {
    if (lower.endsWith('.xlsx')) return await readXlsxDataset(await file.arrayBuffer());
    const content = await file.text();
    if (/DISPIMG/i.test(content) || fileNamesInDataset(content).length > 0) {
      return { content: '', images: [], error: 'csv-image' };
    }
    return { content, images: [] };
  } catch (error) {
    if (error instanceof SpreadsheetImageError) return { content: '', images: [], error: 'csv-image' };
    return { content: '', images: [], error: 'read' };
  }
}

export async function readXlsxDataset(data: ArrayBuffer): Promise<{ content: string; images: PendingImage[] }> {
  const files = await unzip(data);
  const sheetPath = firstSheetPath(files);
  const rows = sheetRows(text(files, sheetPath), sharedStrings(text(files, 'xl/sharedStrings.xml')));
  const embedded = embeddedImages(files);
  const anchored = drawingImages(files, sheetPath);
  const images: PendingImage[] = [];
  const offset = headerOffset(rows);
  const cases = rows.slice(offset).flatMap((row) => {
    const record = rowRecord(offset ? rows[0] : undefined, row);
    const pictures = rowImages(row, embedded, anchored.get(row.row) ?? [], images);
    if (!record.query && pictures.length === 0) return [];
    return [pictures.length === 0 ? record : { ...record, files: pictures }];
  });
  return { content: `${JSON.stringify(cases)}\n`, images };
}

function headerOffset(rows: SheetRow[]) {
  const header = rows[0]?.cells.map((cell) => cell.text.trim().toLowerCase()) ?? [];
  return header.includes('query') ? 1 : 0;
}

function rowRecord(header: SheetRow | undefined, row: SheetRow) {
  const columns = new Map<string, string>();
  const headerCells = new Map((header?.cells ?? []).map((cell) => [cell.column, cell.text.trim().toLowerCase()]));
  for (const cell of row.cells) {
    const name = headerCells.get(cell.column);
    if (name) columns.set(name, cell.text);
  }
  const trace = columns.get('expected_tool_trace') ?? '';
  const answer = answerText(columns);
  const polarity = columns.get('polarity') ?? '';
  const acceptance = columns.get('acceptance') ?? '';
  return {
    query: columns.get('query') ?? row.cells.find((cell) => cell.column === 0)?.text ?? '',
    ...(answer ? { expected_answer: answer } : {}),
    expected_tool_trace: trace.startsWith('[') || trace.startsWith('{') ? parseJson(trace) : undefined,
    ...(polarity ? { polarity } : {}),
    ...(acceptance ? { acceptance } : {}),
  };
}

function answerText(columns: Map<string, string>) {
  return columns.get('expected_answer') ?? columns.get('expectedanswer') ?? '';
}

function rowImages(
  row: SheetRow,
  embedded: Map<string, PendingImage>,
  anchored: PendingImage[],
  images: PendingImage[],
) {
  const names: string[] = [];
  const add = (image: PendingImage) => {
    if (names.includes(image.name)) return;
    if (!images.some((item) => item.name === image.name)) images.push(image);
    names.push(image.name);
  };
  for (const cell of row.cells) {
    const id = cell.formula.match(/DISPIMG\(\s*"([^"]+)"/i)?.[1];
    if (!id) continue;
    const image = embedded.get(id);
    if (!image) throw new SpreadsheetImageError();
    add(image);
  }
  for (const image of anchored) add(image);
  return names;
}

function embeddedImages(files: Map<string, Uint8Array>) {
  const images = new Map<string, PendingImage>();
  const rels = text(files, 'xl/_rels/cellimages.xml.rels');
  const xml = text(files, 'xl/cellimages.xml');
  for (const chunk of xml.split(/cellImage/i).slice(1)) {
    const name = chunk.match(/name="(ID_[^"]+)"/)?.[1];
    const embed = chunk.match(/r:embed="([^"]+)"/)?.[1];
    const target = embed ? relationshipTarget(rels, embed) : '';
    const image = target ? mediaImage(files, joinZip('xl/cellimages.xml', target), name ?? target) : undefined;
    if (name && image) images.set(name, image);
  }
  return images;
}

function drawingImages(files: Map<string, Uint8Array>, sheetPath: string) {
  const byRow = new Map<number, PendingImage[]>();
  const sheetRels = text(files, relsPath(sheetPath));
  const drawing = relationshipTarget(sheetRels, drawingRelationship(sheetRels));
  if (!drawing) return byRow;
  const drawingPath = joinZip(sheetPath, drawing);
  const drawingXml = text(files, drawingPath);
  const drawingRels = text(files, relsPath(drawingPath));
  for (const anchor of drawingXml.split(/<(?:\w+:)?(?:twoCellAnchor|oneCellAnchor)/i).slice(1)) {
    const row = Number(anchor.match(/<(?:\w+:)?from>[\s\S]*?<(?:\w+:)?row>(\d+)/)?.[1]);
    const embed = anchor.match(/r:embed="([^"]+)"/)?.[1] ?? '';
    const target = relationshipTarget(drawingRels, embed);
    const image = target ? mediaImage(files, joinZip(drawingPath, target), target) : undefined;
    if (!image || Number.isNaN(row)) continue;
    const sheetRow = row + 1;
    byRow.set(sheetRow, [...(byRow.get(sheetRow) ?? []), image]);
  }
  return byRow;
}

function mediaImage(files: Map<string, Uint8Array>, path: string, name: string): PendingImage | undefined {
  const bytes = files.get(path);
  const mime = mimeType(path);
  const kind = blockTypeForMime(mime);
  if (!bytes || !kind) return undefined;
  return { name: path.split('/').pop() ?? name, bytes, mime, kind };
}

function mimeType(path: string) {
  const extension = path.split('.').pop()?.toLowerCase();
  if (extension === 'png') return 'image/png';
  if (extension === 'jpg' || extension === 'jpeg') return 'image/jpeg';
  if (extension === 'gif') return 'image/gif';
  if (extension === 'webp') return 'image/webp';
  if (extension === 'pdf') return 'application/pdf';
  if (extension === 'txt') return 'text/plain';
  return '';
}

function sheetRows(sheet: string, strings: string[]) {
  const rows: SheetRow[] = [];
  for (const match of sheet.matchAll(/<c\b([^>]*?)\s*(?:\/>|>([\s\S]*?)<\/c>)/g)) {
    const ref = match[1]?.match(/\br="([A-Z]+)(\d+)"/) ?? [];
    const row = Number(ref[2]);
    const column = columnIndex(ref[1] ?? '');
    if (!row) continue;
    const inner = match[2] ?? '';
    const formula = decodeXml(inner.match(/<f[^>]*>([\s\S]*?)<\/f>/)?.[1] ?? '');
    const type = match[1]?.match(/\bt="([^"]+)"/)?.[1] ?? '';
    const value = decodeXml(inner.match(/<v[^>]*>([\s\S]*?)<\/v>/)?.[1] ?? '');
    const inline = [...inner.matchAll(/<t[^>]*>([\s\S]*?)<\/t>/g)].map((item) => decodeXml(item[1] ?? '')).join('');
    const text = inline || (type === 's' ? (strings[Number(value)] ?? '') : value);
    const current = rows.find((item) => item.row === row) ?? { row, cells: [] };
    if (!rows.includes(current)) rows.push(current);
    current.cells.push({ column, text, formula });
  }
  return rows.sort((left, right) => left.row - right.row);
}

function sharedStrings(xml: string) {
  return [...xml.matchAll(/<si\b[^>]*>([\s\S]*?)<\/si>/g)].map((item) =>
    [...(item[1] ?? '').matchAll(/<t[^>]*>([\s\S]*?)<\/t>/g)].map((text) => decodeXml(text[1] ?? '')).join(''),
  );
}

function firstSheetPath(files: Map<string, Uint8Array>) {
  const workbook = text(files, 'xl/workbook.xml');
  const rels = text(files, 'xl/_rels/workbook.xml.rels');
  const id = workbook.match(/<sheet\b[^>]*r:id="([^"]+)"/)?.[1] ?? '';
  const target = relationshipTarget(rels, id);
  return target ? joinZip('xl/workbook.xml', target) : 'xl/worksheets/sheet1.xml';
}

function drawingRelationship(rels: string) {
  const match = rels.match(/Type="[^"]*\/drawing"[^>]*Id="([^"]+)"|Id="([^"]+)"[^>]*Type="[^"]*\/drawing"/);
  return match?.[1] || match?.[2] || '';
}

function relationshipTarget(rels: string, id: string) {
  if (!id) return '';
  const tags = [...rels.matchAll(/<Relationship\b[^>]*>/g)].map((item) => item[0] ?? '');
  const tag = tags.find((item) => item.includes(`Id="${id}"`));
  return tag?.match(/\bTarget="([^"]+)"/)?.[1] ?? '';
}

function relsPath(part: string) {
  const slash = part.lastIndexOf('/');
  const dir = slash < 0 ? '' : part.slice(0, slash);
  const name = part.slice(slash + 1);
  return `${dir}/_rels/${name}.rels`;
}

function joinZip(part: string, target: string) {
  if (target.startsWith('/')) return target.slice(1);
  const slash = part.lastIndexOf('/');
  const parts = (slash < 0 ? '' : part.slice(0, slash)).split('/').filter(Boolean);
  for (const piece of target.split('/')) {
    if (piece === '..') parts.pop();
    else if (piece && piece !== '.') parts.push(piece);
  }
  return parts.join('/');
}

function text(files: Map<string, Uint8Array>, path: string) {
  const bytes = files.get(path);
  return bytes ? new TextDecoder().decode(bytes) : '';
}

function decodeXml(value: string) {
  return value
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&quot;', '"')
    .replaceAll('&apos;', "'")
    .replaceAll('&amp;', '&');
}

function columnIndex(letters: string) {
  return [...letters].reduce((index, letter) => index * 26 + letter.charCodeAt(0) - 64, 0) - 1;
}

function parseJson(value: string) {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return value;
  }
}

function sharedStringsXml(values: string[]) {
  const items = values.map((value) => `<si><t>${value}</t></si>`).join('');
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="${values.length}" uniqueCount="${values.length}">${items}</sst>`;
}

function xml(value: string) {
  return new TextEncoder().encode(value);
}

async function unzip(data: ArrayBuffer) {
  const bytes = new Uint8Array(data);
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const eocd = findEocd(bytes);
  const count = view.getUint16(eocd + 10, true);
  let cursor = view.getUint32(eocd + 16, true);
  const files = new Map<string, Uint8Array>();
  for (let index = 0; index < count; index += 1) {
    const nameLength = view.getUint16(cursor + 28, true);
    const extraLength = view.getUint16(cursor + 30, true);
    const commentLength = view.getUint16(cursor + 32, true);
    const name = new TextDecoder().decode(bytes.subarray(cursor + 46, cursor + 46 + nameLength));
    const local = view.getUint32(cursor + 42, true);
    const localName = view.getUint16(local + 26, true);
    const localExtra = view.getUint16(local + 28, true);
    const start = local + 30 + localName + localExtra;
    const size = view.getUint32(cursor + 20, true);
    const method = view.getUint16(cursor + 10, true);
    const compressed = bytes.subarray(start, start + size);
    files.set(name, method === 0 ? compressed : await inflateRaw(compressed));
    cursor += 46 + nameLength + extraLength + commentLength;
  }
  return files;
}

function findEocd(bytes: Uint8Array) {
  const start = Math.max(0, bytes.length - 22 - 65535);
  for (let index = bytes.length - 22; index >= start; index -= 1) {
    if (bytes[index] === 0x50 && bytes[index + 1] === 0x4b && bytes[index + 2] === 0x05 && bytes[index + 3] === 0x06) {
      return index;
    }
  }
  throw new Error('xlsx is not a zip archive');
}

function blobFromBytes(data: Uint8Array) {
  const buffer = new ArrayBuffer(data.byteLength);
  new Uint8Array(buffer).set(data);
  return new Blob([buffer]);
}

async function inflateRaw(data: Uint8Array) {
  const stream = blobFromBytes(data).stream().pipeThrough(new DecompressionStream('deflate-raw'));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

type ZipEntry = { name: string; data: Uint8Array; compressed: Uint8Array; method: number };

function zipStored(files: Record<string, Uint8Array>) {
  return zipEntries(Object.entries(files).map(([name, data]) => ({ name, data, compressed: data, method: 0 })));
}

async function zipCompressed(files: Record<string, Uint8Array>) {
  const entries = await Promise.all(
    Object.entries(files).map(async ([name, data]) => ({
      name,
      data,
      compressed: await deflateRaw(data),
      method: 8,
    })),
  );
  return zipEntries(entries);
}

function zipEntries(entries: ZipEntry[]) {
  const locals: Uint8Array[] = [];
  const centrals: Uint8Array[] = [];
  let offset = 0;
  for (const entry of entries) {
    const name = new TextEncoder().encode(entry.name);
    const local = localHeader(name, entry);
    locals.push(local);
    centrals.push(centralHeader(name, entry, offset));
    offset += local.length;
  }
  const directory = concat(centrals);
  const end = new Uint8Array(22);
  const view = new DataView(end.buffer);
  view.setUint32(0, 0x06054b50, true);
  view.setUint16(8, centrals.length, true);
  view.setUint16(10, centrals.length, true);
  view.setUint32(12, directory.length, true);
  view.setUint32(16, offset, true);
  return concat([...locals, directory, end]);
}

function localHeader(name: Uint8Array, entry: ZipEntry) {
  const header = new Uint8Array(30 + name.length + entry.compressed.length);
  const view = new DataView(header.buffer);
  view.setUint32(0, 0x04034b50, true);
  view.setUint16(4, 20, true);
  view.setUint16(8, entry.method, true);
  writeSizes(view, 14, name, entry);
  header.set(name, 30);
  header.set(entry.compressed, 30 + name.length);
  return header;
}

function centralHeader(name: Uint8Array, entry: ZipEntry, offset: number) {
  const header = new Uint8Array(46 + name.length);
  const view = new DataView(header.buffer);
  view.setUint32(0, 0x02014b50, true);
  view.setUint16(6, 20, true);
  view.setUint16(10, entry.method, true);
  writeSizes(view, 16, name, entry);
  view.setUint32(42, offset, true);
  header.set(name, 46);
  return header;
}

function writeSizes(view: DataView, crcOffset: number, name: Uint8Array, entry: ZipEntry) {
  view.setUint32(crcOffset, crc32(entry.data), true);
  view.setUint32(crcOffset + 4, entry.compressed.length, true);
  view.setUint32(crcOffset + 8, entry.data.length, true);
  view.setUint16(crcOffset + 12, name.length, true);
}

async function deflateRaw(data: Uint8Array) {
  const stream = blobFromBytes(data).stream().pipeThrough(new CompressionStream('deflate-raw'));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

function concat(parts: Uint8Array[]) {
  const size = parts.reduce((total, part) => total + part.length, 0);
  const output = new Uint8Array(size);
  let offset = 0;
  for (const part of parts) {
    output.set(part, offset);
    offset += part.length;
  }
  return output;
}

function crc32(data: Uint8Array) {
  let crc = ~0;
  for (const byte of data) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
  }
  return ~crc >>> 0;
}
