const defaultReturnTo = '/';

export function normalizeReturnTo(value: string | null | undefined) {
  if (!value) {
    return defaultReturnTo;
  }

  const trimmed = value.trim();
  if (!trimmed.startsWith('/') || trimmed.startsWith('//') || trimmed.includes('\\')) {
    return defaultReturnTo;
  }

  try {
    const url = new URL(trimmed, 'https://open-managed-agent.local');
    if (url.origin !== 'https://open-managed-agent.local') {
      return defaultReturnTo;
    }
    if (url.pathname === '/login') {
      return defaultReturnTo;
    }
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    return defaultReturnTo;
  }
}

export function returnToFromSearch(search: string) {
  const normalizedSearch = search.startsWith('?') ? search.slice(1) : search;
  const params = new URLSearchParams(normalizedSearch);
  return normalizeReturnTo(params.get('returnTo'));
}

export function loginHrefForReturnTo(returnTo: string) {
  return `/login?returnTo=${encodeURIComponent(normalizeReturnTo(returnTo))}`;
}
