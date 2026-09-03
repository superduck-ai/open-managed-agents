import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Ban } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { TooltipProvider } from '../../shared/ui/tooltip';
import { useWorkspace } from '../../shared/workspaces/context';
import { getObservabilityDashboard } from './api';
import { ObservabilityFiltersBar } from './filters';
import {
  clampedCustomRange,
  defaultObservabilityFilters,
  isApiError,
  panelIdFromSearch,
  panelVariablesFromFilters,
  refreshedTimeRange,
  tabForPanelId,
  tabFromSearch,
  traceIdFromSearch,
  writeWorkspaceObservabilitySearch,
  zoomOutTimeRange,
  type ObservabilityFilters,
} from './model';
import { ObservabilityStatus } from './ObservabilityStatus';
import { PanelGrid, PanelGridSkeleton } from './PanelGrid';
import { ObservabilityToolbar } from './toolbar';
import { TraceDetailView } from './traces/TraceDetailView';
import { TraceListView } from './traces/TraceListView';
import type { ObservabilityDashboard, ObservabilityScope, ObservabilityTabId, PanelQueryVariables } from './types';

const TAB_IDS: ObservabilityTabId[] = ['overview', 'model', 'tool', 'traces'];

export function ObservabilityPage({ scope }: { scope: ObservabilityScope }) {
  const { msg } = useI18n();
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const queryClient = useQueryClient();
  const workspaceSearch = scope.kind === 'workspace' && typeof window !== 'undefined';
  // URL 状态只在挂载时解析一次；用惰性初始化避免每次渲染都重新解析 location.search。
  const [initial] = useState(() => workspaceSearchState(workspaceSearch));
  const [tab, setTab] = useState<ObservabilityTabId>(initial.tab);
  const [openTraceId, setOpenTraceId] = useState<string | null>(initial.traceId);
  const [viewedPanelId, setViewedPanelId] = useState<string | null>(initial.panelId);
  const [filters, setFilters] = useState<ObservabilityFilters>(defaultObservabilityFilters);
  const dashboardQuery = useQuery({
    queryKey: ['observability', 'dashboard', orgUuid, activeWorkspaceId],
    queryFn: () => getObservabilityDashboard(orgUuid ?? '', activeWorkspaceId),
    enabled: Boolean(orgUuid) && Boolean(activeWorkspaceId),
    retry: false,
    refetchOnWindowFocus: false,
    // dashboard 定义随部署变化，不随数据变化，会话内基本静态。
    staleTime: 5 * 60 * 1000,
  });
  const agentId = scope.kind === 'agent' ? scope.agentId : undefined;
  const sessionId = scope.kind === 'session' ? scope.sessionId : undefined;
  const variables = useMemo(
    () =>
      panelVariablesFromFilters(
        filters,
        { agentId, sessionId },
        {
          includeModel: tab === 'model',
          includeTool: tab === 'tool',
        },
      ),
    [agentId, filters, sessionId, tab],
  );
  const dashboard = dashboardQuery.data;

  // URL 直达某个面板时把 tab 对齐到面板所在页；条件性的渲染期状态调整，
  // 比 effect 少一轮级联渲染（参见 React "adjusting state during render"）。
  if (viewedPanelId && dashboard) {
    const nextTab = tabForPanelId(dashboard.tabs, viewedPanelId);
    if (!nextTab) {
      setViewedPanelId(null);
    } else if (nextTab !== tab) {
      setTab(nextTab);
    }
  }

  useEffect(() => {
    if (!viewedPanelId) {
      return;
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setViewedPanelId(null);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [viewedPanelId]);

  useEffect(() => {
    if (scope.kind !== 'workspace') {
      return;
    }
    writeWorkspaceObservabilitySearch(tab, openTraceId, viewedPanelId);
  }, [openTraceId, scope.kind, tab, viewedPanelId]);

  const loadStatus = observabilityPageLoadStatus(orgUuid, dashboardQuery, msg);
  if (loadStatus || !orgUuid) {
    return loadStatus;
  }

  const applyTimeRange = (start: string, end: string) => {
    const next = clampedCustomRange(start, end);
    if (next) {
      setFilters((current) => ({ ...current, ...next }));
    }
  };
  const zoomOut = () => {
    setFilters((current) => ({ ...current, ...zoomOutTimeRange(current.start, current.end) }));
  };
  const toggleView = (panelId: string) => {
    const next = viewedPanelId === panelId ? null : panelId;
    setViewedPanelId(next);
    setOpenTraceId(null);
  };

  return (
    <TooltipProvider>
      <section className="flex min-w-0 flex-col">
        {openTraceId ? (
          <TraceDetailView
            orgUuid={orgUuid}
            traceId={openTraceId}
            variables={variables}
            onClose={() => {
              setOpenTraceId(null);
            }}
          />
        ) : (
          <>
            {scope.kind === 'workspace' ? (
              <h1 className="mb-5 text-[28px] font-semibold leading-tight tracking-tight text-foreground">
                {msg('observability.title', 'Observability')}
              </h1>
            ) : null}
            <div className="mb-6">
              <ObservabilityToolbar
                filters={filters}
                onChange={setFilters}
                onRefresh={() => {
                  setFilters((current) => refreshedTimeRange(current));
                  void queryClient.invalidateQueries({ queryKey: ['observability'] });
                }}
                refreshing={dashboardQuery.isFetching}
              >
                <ObservabilityFiltersBar filters={filters} onChange={setFilters} scope={scope} tab={tab} />
              </ObservabilityToolbar>
            </div>
            <Tabs
              value={tab}
              onValueChange={(next) => {
                const nextTab = tabFromSearch(next);
                setTab(nextTab);
                setOpenTraceId(null);
                setViewedPanelId(null);
              }}
              className="gap-4"
            >
              {viewedPanelId ? null : (
                <TabsList variant="line">
                  {TAB_IDS.map((id) => (
                    <TabsTrigger key={id} value={id}>
                      {tabLabel(id, msg)}
                    </TabsTrigger>
                  ))}
                </TabsList>
              )}
              <AnalysisTabPanels
                dashboard={dashboard}
                pending={dashboardQuery.isPending}
                tab={tab}
                orgUuid={orgUuid}
                variables={variables}
                viewedPanelId={viewedPanelId}
                onToggleView={toggleView}
                onTimeRangeChange={applyTimeRange}
                onTimeRangeZoomOut={zoomOut}
              />
              <TabsContent value="traces">
                {/* 等 dashboard 就绪再渲染：否则 trend 面板拿不到变量声明，会把
                    agent/session 作用域从查询里丢掉，短暂展示越scope的数据。 */}
                <TracesTabBody
                  tab={tab}
                  dashboard={dashboard}
                  pending={dashboardQuery.isPending}
                  orgUuid={orgUuid}
                  filters={filters}
                  variables={variables}
                  scope={scope}
                  viewedPanelId={viewedPanelId}
                  onToggleView={toggleView}
                  onOpenTrace={(traceId) => {
                    setTab('traces');
                    setOpenTraceId(traceId);
                    setViewedPanelId(null);
                  }}
                  onTimeRangeChange={applyTimeRange}
                  onTimeRangeZoomOut={zoomOut}
                />
              </TabsContent>
            </Tabs>
          </>
        )}
      </section>
    </TooltipProvider>
  );
}

function workspaceSearchState(enabled: boolean) {
  if (!enabled) {
    return { tab: 'overview' as ObservabilityTabId, traceId: null as string | null, panelId: null as string | null };
  }
  const search = new URLSearchParams(window.location.search);
  const traceId = traceIdFromSearch(search.get('trace_id'));
  return {
    tab: traceId ? ('traces' as const) : tabFromSearch(search.get('tab')),
    traceId,
    panelId: traceId ? null : panelIdFromSearch(search.get('panel')),
  };
}

function tabLabel(id: ObservabilityTabId, msg: ReturnType<typeof useI18n>['msg']) {
  if (id === 'overview') return msg('observability.tab.overview', 'Overview');
  if (id === 'model') return msg('observability.tab.model', 'Models');
  if (id === 'tool') return msg('observability.tab.tool', 'Tools');
  return msg('observability.tab.traces', 'Traces');
}

function observabilityPageLoadStatus(
  orgUuid: string | undefined,
  dashboardQuery: { isError: boolean; error: unknown; refetch: () => Promise<unknown> },
  msg: ReturnType<typeof useI18n>['msg'],
) {
  if (!orgUuid) {
    return (
      <ObservabilityStatus
        tone="error"
        size="page"
        title={msg('observability.loadError', 'Couldn’t load observability')}
      />
    );
  }
  if (!dashboardQuery.isError) {
    return null;
  }
  if (isApiError(dashboardQuery.error) && dashboardQuery.error.status === 404) {
    return (
      <ObservabilityStatus
        size="page"
        icon={Ban}
        title={msg('observability.disabled.title', 'Observability is not enabled')}
        description={msg(
          'observability.disabled.body',
          'Turn on observability in server configuration to query traces and metrics.',
        )}
      />
    );
  }
  return (
    <ObservabilityStatus
      tone="error"
      size="page"
      title={msg('observability.loadError', 'Couldn’t load observability')}
      actionLabel={msg('observability.retry', 'Retry')}
      onAction={() => void dashboardQuery.refetch()}
    />
  );
}

function AnalysisTabPanels({
  dashboard,
  pending,
  tab,
  orgUuid,
  variables,
  viewedPanelId,
  onToggleView,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  dashboard?: ObservabilityDashboard;
  pending: boolean;
  tab: ObservabilityTabId;
  orgUuid: string;
  variables: PanelQueryVariables;
  viewedPanelId: string | null;
  onToggleView: (panelId: string) => void;
  onTimeRangeChange: (start: string, end: string) => void;
  onTimeRangeZoomOut: () => void;
}) {
  if (dashboard) {
    return dashboard.tabs.map((dashboardTab) => (
      <TabsContent key={dashboardTab.id} value={dashboardTab.id}>
        {tab === dashboardTab.id ? (
          <PanelGrid
            orgUuid={orgUuid}
            panels={dashboardTab.panels}
            queries={dashboard.queries}
            variables={variables}
            viewedPanelId={viewedPanelId}
            onToggleView={onToggleView}
            onTimeRangeChange={onTimeRangeChange}
            onTimeRangeZoomOut={onTimeRangeZoomOut}
          />
        ) : null}
      </TabsContent>
    ));
  }
  if (!pending) {
    return null;
  }
  return TAB_IDS.filter((id) => id !== 'traces').map((id) => (
    <TabsContent key={id} value={id}>
      {tab === id ? <PanelGridSkeleton /> : null}
    </TabsContent>
  ));
}

function TracesTabBody({
  tab,
  dashboard,
  pending,
  orgUuid,
  filters,
  variables,
  scope,
  viewedPanelId,
  onToggleView,
  onOpenTrace,
  onTimeRangeChange,
  onTimeRangeZoomOut,
}: {
  tab: ObservabilityTabId;
  dashboard?: ObservabilityDashboard;
  pending: boolean;
  orgUuid: string;
  filters: ObservabilityFilters;
  variables: PanelQueryVariables;
  scope: ObservabilityScope;
  viewedPanelId: string | null;
  onToggleView: (panelId: string) => void;
  onOpenTrace: (traceId: string) => void;
  onTimeRangeChange: (start: string, end: string) => void;
  onTimeRangeZoomOut: () => void;
}) {
  if (tab !== 'traces') {
    return null;
  }
  if (dashboard) {
    return (
      <TraceListView
        orgUuid={orgUuid}
        filters={filters}
        variables={variables}
        queries={dashboard.queries}
        showTrends
        showAgentColumn={scope.kind === 'workspace'}
        showSessionColumn={scope.kind !== 'session'}
        viewedPanelId={viewedPanelId}
        onToggleView={onToggleView}
        onOpenTrace={onOpenTrace}
        onTimeRangeChange={onTimeRangeChange}
        onTimeRangeZoomOut={onTimeRangeZoomOut}
      />
    );
  }
  if (pending) {
    return <PanelGridSkeleton count={3} />;
  }
  return null;
}
