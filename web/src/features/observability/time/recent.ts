import type { RawTimeRange } from './range';

const STORAGE_KEY = 'oma.observability.recentRanges';
const LIMIT = 4;

export function readRecentRanges(): RawTimeRange[] {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(STORAGE_KEY) ?? '[]');
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((item): item is RawTimeRange => typeof item?.from === 'string' && typeof item?.to === 'string')
      .slice(0, LIMIT);
  } catch {
    return [];
  }
}

export function rememberRange(raw: RawTimeRange) {
  const next = [raw, ...readRecentRanges().filter((item) => item.from !== raw.from || item.to !== raw.to)].slice(
    0,
    LIMIT,
  );
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
  } catch {
    return;
  }
}
