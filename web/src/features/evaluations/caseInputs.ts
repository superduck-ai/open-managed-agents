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
