import { useQuery } from '@tanstack/react-query';
import { useEffect, useMemo, useState } from 'react';
import { useWorkspace } from '../../shared/workspaces/context';
import { listTrajectories, type ObservabilityScope } from './api';

type Selection = { kind: 'undecided' } | { kind: 'list' } | { kind: 'open'; id: string };
type BrowserState = { scope: string; selection: Selection; offsets: number[] };

function initialState(scope: string): BrowserState {
  return { scope, selection: { kind: 'undecided' }, offsets: [0] };
}

export function useTrajectoryBrowser(scope: ObservabilityScope) {
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const sessionId = scope.kind === 'session' ? scope.sessionId : undefined;
  const agentId = scope.kind === 'agent' ? scope.agentId : undefined;
  const queryScope = useMemo(
    () => ({ orgUuid: orgUuid ?? '', workspaceId: activeWorkspaceId, sessionId, agentId }),
    [orgUuid, activeWorkspaceId, sessionId, agentId],
  );
  const scopeKey = JSON.stringify(queryScope);
  const [state, setState] = useState(() => initialState(scopeKey));
  const active = state.scope === scopeKey ? state : initialState(scopeKey);
  const offset = active.offsets.at(-1)!;
  const query = useQuery({
    queryKey: ['trajectories', queryScope, offset],
    queryFn: () => listTrajectories(queryScope, offset),
    enabled: !!orgUuid && !!activeWorkspaceId && active.selection.kind !== 'open',
    refetchInterval: 3000,
    retry: false,
  });
  useEffect(() => {
    setState((current) => (current.scope === scopeKey ? current : initialState(scopeKey)));
  }, [scopeKey]);
  useEffect(() => {
    if (scope.kind !== 'session' || !query.data?.items.length) return;
    const id = query.data.items[0].id;
    setState((current) =>
      current.scope === scopeKey && current.selection.kind === 'undecided'
        ? { ...current, selection: { kind: 'open', id } }
        : current,
    );
  }, [scope.kind, scopeKey, query.data]);
  const select = (id: string | null) =>
    setState({
      ...active,
      selection: id === null ? { kind: 'list' } : { kind: 'open', id },
    });
  const next = () => {
    const nextOffset = query.data?.next_offset;
    if (nextOffset != null) setState({ ...active, offsets: [...active.offsets, nextOffset] });
  };
  const previous = () => {
    if (active.offsets.length > 1) setState({ ...active, offsets: active.offsets.slice(0, -1) });
  };
  return {
    queryScope,
    scopeKey,
    current: active.selection.kind === 'open' ? active.selection.id : undefined,
    select,
    query,
    next,
    previous,
    hasPrevious: active.offsets.length > 1,
    ready: !!orgUuid && !!activeWorkspaceId,
  };
}
