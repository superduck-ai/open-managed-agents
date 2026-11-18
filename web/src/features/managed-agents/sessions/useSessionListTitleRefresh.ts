import { useEffect, type RefObject } from 'react';
import { listManagedEntities } from '../api';
import type {
  ManagedEntityApiResponse,
  ManagedEntityListFilters,
  ManagedEntitySection,
  PageCursor,
  PageResponse,
} from '../types';
import { sessionTitleRefreshInterval } from './sessionTitleRefresh';

function listTitleRefreshInterval(entities: ManagedEntityApiResponse[]) {
  let shortest = Infinity;
  for (const entity of entities) {
    if (entity.type !== 'session') continue;
    const interval = sessionTitleRefreshInterval(entity);
    if (interval) shortest = Math.min(shortest, interval);
  }
  return shortest;
}

export function useSessionListTitleRefresh({
  section,
  loading,
  searching,
  entities,
  workspaceId,
  cursor,
  filters,
  pageRevision,
  onPage,
}: {
  section: ManagedEntitySection;
  loading: boolean;
  searching: boolean;
  entities: ManagedEntityApiResponse[];
  workspaceId: string;
  cursor: PageCursor;
  filters: ManagedEntityListFilters | undefined;
  pageRevision: RefObject<number>;
  onPage: (page: PageResponse<ManagedEntityApiResponse>) => void;
}) {
  useEffect(() => {
    if (section !== 'sessions' || loading || searching || !Number.isFinite(listTitleRefreshInterval(entities))) return;
    let active = true;
    let pending = false;
    const refresh = async () => {
      if (document.visibilityState !== 'visible' || pending) return;
      pending = true;
      const revision = pageRevision.current;
      try {
        const page = await listManagedEntities('sessions', workspaceId, cursor, filters);
        if (active && revision === pageRevision.current) onPage(page);
      } catch {
        return;
      } finally {
        pending = false;
      }
    };
    let timer: number;
    const schedule = () => {
      timer = window.setTimeout(async () => {
        await refresh();
        if (active) schedule();
      }, listTitleRefreshInterval(entities));
    };
    schedule();
    const onVisible = () => void refresh();
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      active = false;
      window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [section, loading, searching, entities, workspaceId, cursor, filters, pageRevision, onPage]);
}
