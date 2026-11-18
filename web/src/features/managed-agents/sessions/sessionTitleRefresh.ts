import type { SessionApiResponse } from '../types';

const SESSION_TITLE_REFRESH_INTERVAL_MS = 5000;
const SESSION_TITLE_REFRESH_IDLE_INTERVAL_MS = 30000;
const SESSION_TITLE_REFRESH_WINDOW_MS = 10 * 60 * 1000;

export function sessionTitleRefreshInterval(session: SessionApiResponse, now = Date.now()) {
  if (session.title || session.archived_at) return false;
  const age = now - Date.parse(session.updated_at);
  return age >= 0 && age < SESSION_TITLE_REFRESH_WINDOW_MS
    ? SESSION_TITLE_REFRESH_INTERVAL_MS
    : SESSION_TITLE_REFRESH_IDLE_INTERVAL_MS;
}
