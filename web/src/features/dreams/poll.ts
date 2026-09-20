import type { Dream, DreamStatus } from './api';

export const DREAM_ACTIVE_POLL_MS = 3_000;

export type DreamDrawerView = 'history' | 'create' | 'detail';

export function dreamIsActive(status: DreamStatus) {
  return status === 'pending' || status === 'running';
}

export function mergeDreamList(current: Dream[], latest: Dream[]) {
  const latestIds = new Set(latest.map((dream) => dream.id));
  return [...latest, ...current.filter((dream) => !latestIds.has(dream.id))];
}

export function shouldPollDreamList(open: boolean, view: DreamDrawerView, dreams: Dream[]) {
  return open && view === 'history' && dreams.some((dream) => dreamIsActive(dream.status));
}

export function shouldPollDreamDetail(open: boolean, view: DreamDrawerView, dream: Dream | null) {
  return open && view === 'detail' && dream != null && dreamIsActive(dream.status);
}
