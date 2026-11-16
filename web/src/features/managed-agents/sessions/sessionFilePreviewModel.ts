const MB = 1024 * 1024;

export const SESSION_FILE_PREVIEW_ROW_LIMIT = 1000;
export const SESSION_FILE_PREVIEW_COLUMN_LIMIT = 50;

const IMAGE_TYPES: Record<string, string> = {
  gif: 'image/gif',
  jpeg: 'image/jpeg',
  jpg: 'image/jpeg',
  png: 'image/png',
  svg: 'image/svg+xml',
  webp: 'image/webp',
};

const CODE_EXTENSIONS =
  'bash c cfg conf cpp css go h ini java js json jsx log mjs py rb rs sh sql toml ts tsx txt xml yaml yml zsh';

const EXTENSION_KINDS: Record<string, SessionFilePreviewKind> = {
  ...Object.fromEntries(CODE_EXTENSIONS.split(' ').map((extension) => [extension, 'code'])),
  ...Object.fromEntries(Object.keys(IMAGE_TYPES).map((extension) => [extension, 'image'])),
  csv: 'csv',
  docx: 'docx',
  htm: 'html',
  html: 'html',
  markdown: 'markdown',
  md: 'markdown',
  pdf: 'pdf',
  tsv: 'csv',
  xlsx: 'xlsx',
};

const SIZE_LIMITS: Record<SessionFilePreviewKind, number> = {
  code: MB,
  csv: MB,
  docx: 10 * MB,
  html: 5 * MB,
  image: 20 * MB,
  markdown: MB,
  pdf: 20 * MB,
  xlsx: 10 * MB,
};

export type SessionFilePreviewKind = 'code' | 'csv' | 'docx' | 'html' | 'image' | 'markdown' | 'pdf' | 'xlsx';

export type SessionFilePreviewSheet = { name: string; rows: string[][]; totalRows: number; totalColumns: number };

export type SessionFilePreviewContent =
  | { kind: 'code'; text: string; language: string }
  | { kind: 'markdown'; text: string }
  | { kind: 'table'; sheets: SessionFilePreviewSheet[] }
  | { kind: 'frame'; blob: Blob; sandbox?: string }
  | { kind: 'image'; blob: Blob };

function fileExtension(filename: string) {
  return /\.([a-z0-9]+)$/i.exec(filename)?.[1].toLowerCase() ?? '';
}

export function sessionFilePreviewKind(file: { filename: string; size_bytes: number; downloadable?: boolean }) {
  const kind = EXTENSION_KINDS[fileExtension(file.filename)];
  return kind && file.downloadable && file.size_bytes <= SIZE_LIMITS[kind] ? kind : null;
}

export function parseDelimitedText(text: string, delimiter: string) {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = '';
  let quoted = false;
  const endRow = () => {
    row.push(cell);
    rows.push(row);
    row = [];
    cell = '';
  };
  for (let index = text.charCodeAt(0) === 0xfeff ? 1 : 0; index < text.length; index++) {
    const char = text[index];
    if (quoted) {
      if (char !== '"') cell += char;
      else if (text[index + 1] === '"') cell += text[++index];
      else quoted = false;
    } else if (char === '"') quoted = true;
    else if (char === delimiter) {
      row.push(cell);
      cell = '';
    } else if (char === '\n' || char === '\r') {
      if (char === '\r' && text[index + 1] === '\n') index++;
      endRow();
    } else cell += char;
  }
  if (cell || row.length) endRow();
  return rows;
}

function previewSheet(name: string, rows: unknown[][]): SessionFilePreviewSheet {
  const shown = rows.slice(0, SESSION_FILE_PREVIEW_ROW_LIMIT);
  return {
    name,
    totalRows: rows.length,
    totalColumns: rows.reduce((widest, row) => Math.max(widest, row.length), 0),
    rows: shown.map((row) =>
      row
        .slice(0, SESSION_FILE_PREVIEW_COLUMN_LIMIT)
        .map((cell) => (cell instanceof Date ? cell.toLocaleString() : String(cell ?? ''))),
    ),
  };
}

const DOCUMENT_FRAME_STYLE =
  '<style>html{color-scheme:light}body{margin:24px;font:14px/1.6 system-ui,sans-serif;color:#1a1a1a;background:#fff;overflow-wrap:anywhere}img{max-width:100%}table{border-collapse:collapse}td,th{border:1px solid #d4d4d4;padding:4px 8px}</style>';

export async function loadSessionFilePreview(
  kind: SessionFilePreviewKind,
  blob: Blob,
  filename: string,
): Promise<SessionFilePreviewContent> {
  const extension = fileExtension(filename);
  switch (kind) {
    case 'code':
      return { kind, text: await blob.text(), language: extension };
    case 'markdown':
      return { kind, text: await blob.text() };
    case 'html':
      return { kind: 'frame', blob: new Blob([blob], { type: 'text/html' }), sandbox: 'allow-scripts' };
    case 'csv':
      return {
        kind: 'table',
        sheets: [previewSheet(filename, parseDelimitedText(await blob.text(), extension === 'tsv' ? '\t' : ','))],
      };
    case 'xlsx': {
      const { default: readXlsxFile } = await import('read-excel-file/universal');
      const sheets = await readXlsxFile(blob);
      return { kind: 'table', sheets: sheets.map(({ sheet, data }) => previewSheet(sheet, data)) };
    }
    case 'docx': {
      const { convertToHtml } = await import('mammoth');
      const { value } = await convertToHtml({ arrayBuffer: await blob.arrayBuffer() });
      const html = `<!doctype html><meta charset="utf-8">${DOCUMENT_FRAME_STYLE}${value}`;
      return { kind: 'frame', blob: new Blob([html], { type: 'text/html' }), sandbox: '' };
    }
    case 'pdf':
      return { kind: 'frame', blob: new Blob([blob], { type: 'application/pdf' }) };
    case 'image':
      return { kind, blob: new Blob([blob], { type: IMAGE_TYPES[extension] }) };
  }
}
