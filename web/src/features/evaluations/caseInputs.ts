export type EvaluationContentBlock =
  { type: 'text'; text: string } | { type: 'image' | 'document'; source: { type: 'file'; file_id: string } };

export type CaseFile = { id: string; kind: 'image' | 'document'; name?: string };

const imageTypes = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp']);
const documentTypes = new Set(['application/pdf', 'text/plain']);

export function blockTypeForMime(mime: string): CaseFile['kind'] | null {
  if (imageTypes.has(mime)) return 'image';
  if (documentTypes.has(mime)) return 'document';
  return null;
}

export function textFromInputs(inputs: EvaluationContentBlock[] | undefined) {
  return (inputs ?? [])
    .filter((block): block is Extract<EvaluationContentBlock, { type: 'text' }> => block.type === 'text')
    .map((block) => block.text)
    .join('\n');
}

export function fileBlocks(inputs: EvaluationContentBlock[] | undefined): CaseFile[] {
  return (inputs ?? []).flatMap((block) => {
    if (block.type === 'text') return [];
    return [{ id: block.source.file_id, kind: block.type }];
  });
}

export function editedCaseInputs(
  original: EvaluationContentBlock[] | undefined,
  query: string,
  files: CaseFile[],
): EvaluationContentBlock[] {
  const blocks = original ?? [];
  const withQuery = query === textFromInputs(blocks) ? blocks : replaceQueryText(blocks, query.trim());
  return sameCaseFiles(fileBlocks(withQuery), files) ? withQuery : syncFileBlocks(withQuery, files);
}

function replaceQueryText(blocks: EvaluationContentBlock[], query: string) {
  const rest = blocks.filter((block) => block.type !== 'text');
  if (!query) return rest;
  const text = { type: 'text' as const, text: query };
  const first = blocks.findIndex((block) => block.type === 'text');
  if (first < 0) return [text, ...rest];
  return [
    ...blocks.slice(0, first).filter((block) => block.type !== 'text'),
    text,
    ...blocks.slice(first + 1).filter((block) => block.type !== 'text'),
  ];
}

function syncFileBlocks(blocks: EvaluationContentBlock[], files: CaseFile[]) {
  const kept = new Set<string>();
  const next = blocks.flatMap((block) => {
    if (block.type === 'text') return [block];
    const file = files.find((item) => item.id === block.source.file_id);
    if (!file || kept.has(file.id)) return [];
    kept.add(file.id);
    return [{ type: file.kind, source: { type: 'file' as const, file_id: file.id } }];
  });
  for (const file of files) {
    if (kept.has(file.id)) continue;
    next.push({ type: file.kind, source: { type: 'file', file_id: file.id } });
  }
  return next;
}

function sameCaseFiles(left: CaseFile[], right: CaseFile[]) {
  return (
    left.length === right.length &&
    left.every((file, index) => file.id === right[index]?.id && file.kind === right[index]?.kind)
  );
}

export function buildInputs(query: string, files: CaseFile[]): EvaluationContentBlock[] {
  const inputs: EvaluationContentBlock[] = [];
  const text = query.trim();
  if (text) inputs.push({ type: 'text', text });
  for (const file of files) {
    inputs.push({ type: file.kind, source: { type: 'file', file_id: file.id } });
  }
  return inputs;
}

export function mediaDetail(
  inputs: EvaluationContentBlock[] | undefined,
  labels: { images: (count: number) => string; documents: (count: number) => string },
) {
  const counts = mediaCounts(inputs);
  return [counts.images ? labels.images(counts.images) : '', counts.documents ? labels.documents(counts.documents) : '']
    .filter(Boolean)
    .join(' · ');
}

export function mediaCounts(inputs: EvaluationContentBlock[] | undefined) {
  return (inputs ?? []).reduce(
    (counts, block) => {
      if (block.type === 'image') counts.images += 1;
      if (block.type === 'document') counts.documents += 1;
      return counts;
    },
    { images: 0, documents: 0 },
  );
}
