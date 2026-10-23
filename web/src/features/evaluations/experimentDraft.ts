export function parseExperimentEnv(value: string): { env?: Record<string, string>; error: boolean } {
  const trimmed = value.trim();
  if (!trimmed) {
    return { error: false };
  }
  if (trimmed.startsWith('{')) {
    return parseExperimentEnvObject(trimmed);
  }
  const env: Record<string, string> = {};
  for (const line of trimmed.split('\n')) {
    const row = line.trim();
    if (!row) {
      continue;
    }
    const separator = row.indexOf('=');
    const key = separator > 0 ? row.slice(0, separator).trim() : '';
    if (!key) {
      return { error: true };
    }
    env[key] = row.slice(separator + 1);
  }
  return { env, error: false };
}

function parseExperimentEnvObject(value: string): { env?: Record<string, string>; error: boolean } {
  let parsed: unknown;
  try {
    parsed = JSON.parse(value);
  } catch {
    return { error: true };
  }
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    return { error: true };
  }
  const env: Record<string, string> = {};
  for (const [key, item] of Object.entries(parsed)) {
    if (typeof item !== 'string' || !key.trim()) {
      return { error: true };
    }
    env[key] = item;
  }
  return { env, error: false };
}

export function parseExperimentResources(value: string): { resources?: unknown[]; error: boolean } {
  const trimmed = value.trim();
  if (!trimmed) {
    return { error: false };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return { error: true };
  }
  if (!Array.isArray(parsed)) {
    return { error: true };
  }
  return { resources: parsed, error: false };
}
