class MissingCaseFilesError extends Error {
  readonly names: string[];

  constructor(names: string[]) {
    super(names.join('、'));
    this.name = 'MissingCaseFilesError';
    this.names = names;
  }
}

export function fileNamesInDataset(content: string) {
  return [...new Set(datasetCases(content).flatMap(fileNamesInCase))];
}

export function missingCaseFileNames(content: string, selected: Array<{ name: string }>) {
  const chosen = new Set(selected.map((file) => file.name.toLowerCase()));
  return fileNamesInDataset(content).filter((name) => !chosen.has(name.toLowerCase()));
}

export function datasetWithUploadedFiles(
  content: string,
  uploaded: Record<string, { id: string; kind: 'image' | 'document' }>,
) {
  const cases = datasetCases(content).map((item) => caseWithUploadedFiles(item, uploaded));
  return `${JSON.stringify(cases)}\n`;
}

function datasetCases(content: string) {
  const trimmed = content.trim();
  if (!trimmed) return [];
  if (trimmed.startsWith('[')) return parseJsonCases(trimmed);
  if (trimmed.startsWith('{')) {
    const envelope = parseJsonCases(trimmed);
    if (envelope.length > 0) return envelope;
    return trimmed
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
      .flatMap(parseJsonCases);
  }
  return parseCsvCases(trimmed);
}

function parseJsonCases(content: string): Array<Record<string, unknown>> {
  try {
    const value = JSON.parse(content) as unknown;
    if (Array.isArray(value)) return value.filter(isRecord);
    if (isRecord(value) && Array.isArray(value.cases)) return value.cases.filter(isRecord);
  } catch {
    return [];
  }
  return [];
}

function parseCsvCases(content: string) {
  const rows = parseCsv(content.replace(/^\uFEFF/, ''));
  const header = rows[0] ?? [];
  return rows.slice(1).flatMap((row) => {
    if (row.every((cell) => cell.trim() === '')) return [];
    const item: Record<string, unknown> = {};
    header.forEach((column, index) => {
      const value = row[index] ?? '';
      if (column === 'expected_tool_trace' && value.trim()) {
        try {
          item[column] = JSON.parse(value) as unknown;
          return;
        } catch {
          item[column] = value;
          return;
        }
      }
      item[column] = value;
    });
    return [item];
  });
}

function caseWithUploadedFiles(
  item: Record<string, unknown>,
  uploaded: Record<string, { id: string; kind: 'image' | 'document' }>,
) {
  const names = fileNamesInCase(item);
  if (names.length === 0) return item;
  const query = typeof item.query === 'string' ? item.query.trim() : '';
  const inputs = [
    ...(query ? [{ type: 'text' as const, text: query }] : []),
    ...names.map((name) => {
      const match = Object.entries(uploaded).find(([key]) => key.toLowerCase() === name.toLowerCase());
      const file = match?.[1];
      if (!file) throw new MissingCaseFilesError([name]);
      return { type: file.kind, source: { type: 'file' as const, file_id: file.id } };
    }),
  ];
  const { files: _files, ...rest } = item;
  return { ...rest, inputs };
}

function fileNamesInCase(item: Record<string, unknown>) {
  const files = item.files;
  if (typeof files === 'string') return splitFileNames(files);
  if (Array.isArray(files)) return files.flatMap((file) => (typeof file === 'string' ? splitFileNames(file) : []));
  return [];
}

function splitFileNames(value: string) {
  return value
    .split(/[;；]/)
    .map((name) => name.trim())
    .filter(Boolean);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function parseCsv(content: string) {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = '';
  let quoted = false;
  for (let index = 0; index < content.length; index += 1) {
    const char = content[index];
    if (quoted) {
      if (char === '"') {
        if (content[index + 1] === '"') {
          cell += '"';
          index += 1;
        } else {
          quoted = false;
        }
      } else {
        cell += char;
      }
      continue;
    }
    if (char === '"') {
      quoted = true;
      continue;
    }
    if (char === ',') {
      row.push(cell);
      cell = '';
      continue;
    }
    if (char === '\n') {
      row.push(cell);
      rows.push(row);
      row = [];
      cell = '';
      continue;
    }
    if (char !== '\r') cell += char;
  }
  if (cell || row.length > 0) {
    row.push(cell);
    rows.push(row);
  }
  return rows;
}
