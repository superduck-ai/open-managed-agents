const returnOrigin = 'https://oma.local';

export function channelSessionsReturnTo(value: string | null | undefined, workspaceId?: string) {
  const trimmed = value?.trim() ?? '';
  if (!trimmed.startsWith('/') || trimmed.startsWith('//') || trimmed.includes('\\')) return null;
  let target: URL;
  try {
    target = new URL(trimmed, returnOrigin);
  } catch {
    return null;
  }
  if (target.origin !== returnOrigin) return null;
  const parts = target.pathname.split('/').filter(Boolean);
  if (parts.length !== 5 || parts[0] !== 'workspaces' || parts[2] !== 'channels') return null;
  if (!parts[1] || !parts[3] || !parts[4]) return null;
  if (workspaceId && decodeURIComponent(parts[1]) !== workspaceId) return null;
  if (target.searchParams.get('tab') !== 'sessions') return null;
  for (const key of target.searchParams.keys()) {
    if (key !== 'tab') return null;
  }
  return `${target.pathname}?tab=sessions`;
}

export function channelSessionsBackHref(search: string, workspaceId: string) {
  const raw = search.startsWith('?') ? search.slice(1) : search;
  return channelSessionsReturnTo(new URLSearchParams(raw).get('returnTo'), workspaceId);
}
