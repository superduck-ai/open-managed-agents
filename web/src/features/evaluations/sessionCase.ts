export function parseSessionCaseCheckpoints(value: string): { checkpoints?: unknown; error?: boolean } {
  const trimmed = value.trim();
  if (!trimmed) {
    return {};
  }
  try {
    const parsed = JSON.parse(trimmed) as unknown;
    if (parsed === null || typeof parsed !== 'object') {
      return { error: true };
    }
    return { checkpoints: parsed };
  } catch {
    return { error: true };
  }
}
