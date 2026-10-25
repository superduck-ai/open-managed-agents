export type EvaluationCaseFile = {
  name: string;
  fileId: string;
};

export function evaluationCaseFiles(value: unknown): EvaluationCaseFile[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value.flatMap((item) => {
    if (!item || typeof item !== 'object') {
      return [];
    }
    const record = item as { name?: unknown; file_id?: unknown };
    const name = typeof record.name === 'string' ? record.name : '';
    const fileId = typeof record.file_id === 'string' ? record.file_id : '';
    if (!name && !fileId) {
      return [];
    }
    return [{ name: name || fileId, fileId }];
  });
}

export function evaluationCaseJSON(value: unknown) {
  if (value == null) {
    return '';
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return '';
  }
}
