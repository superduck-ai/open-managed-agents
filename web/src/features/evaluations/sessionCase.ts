export function parseSessionIDs(value: string) {
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}

export function parseCheckpointsField(value: string): { checkpoints?: string[]; error?: boolean } {
  const trimmed = value.trim();
  if (!trimmed) {
    return {};
  }
  if (trimmed.startsWith('[') || trimmed.startsWith('{')) {
    return parseCheckpointsJSON(trimmed);
  }
  const lines = checkpointLines(trimmed);
  if (lines.length === 0) {
    return {};
  }
  return { checkpoints: lines };
}

export function checkpointsFieldValue(value: unknown) {
  if (Array.isArray(value) && value.every((item) => typeof item === 'string')) {
    return value.join('\n');
  }
  if (value == null) {
    return '';
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return '';
  }
}

function parseCheckpointsJSON(value: string): { checkpoints?: string[]; error?: boolean } {
  try {
    const parsed = JSON.parse(value) as unknown;
    if (!Array.isArray(parsed)) {
      return { error: true };
    }
    const lines: string[] = [];
    for (const item of parsed) {
      if (typeof item !== 'string') {
        return { error: true };
      }
      const line = item.trim();
      if (line.length > 0) {
        lines.push(line);
      }
    }
    if (lines.length === 0) {
      return {};
    }
    return { checkpoints: lines };
  } catch {
    return { error: true };
  }
}

function checkpointLines(value: string) {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}
