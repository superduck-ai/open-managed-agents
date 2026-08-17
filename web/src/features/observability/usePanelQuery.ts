import { useQuery } from '@tanstack/react-query';
import { useWorkspace } from '../../shared/workspaces/context';
import { queryObservabilityPanel } from './api';

// 面板数据是聚合指标，短窗口内重复请求没有意义；切换 tab / 变量时保留旧数据避免闪骨架屏。
const PANEL_STALE_MS = 30_000;

export function usePanelQuery(
  orgUuid: string | undefined,
  queryRef: string,
  variables: Record<string, unknown>,
  enabled = true,
) {
  const { activeWorkspaceId } = useWorkspace();
  return useQuery({
    queryKey: ['observability', 'panel', orgUuid, activeWorkspaceId, queryRef, variables],
    queryFn: () => queryObservabilityPanel(orgUuid ?? '', activeWorkspaceId, queryRef, variables),
    enabled: Boolean(orgUuid) && Boolean(activeWorkspaceId) && Boolean(queryRef) && enabled,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: PANEL_STALE_MS,
    placeholderData: (previousData, previousQuery) =>
      previousQuery?.queryKey[3] === activeWorkspaceId ? previousData : undefined,
  });
}
