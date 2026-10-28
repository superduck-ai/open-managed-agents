import { useMemo, useRef, useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../shared/ui/tabs';
import { TooltipProvider } from '../../shared/ui/tooltip';
import { useWorkspace } from '../../shared/workspaces/context';
import {
  DASHBOARD_TABS,
  type DashboardPanel,
  type DashboardQuery,
  type DashboardTab,
  type ObservabilityScope,
} from './api';
import { DashboardContext, type DashboardInteractions } from './dashboard/context';
import { DashboardToolbar, FilterVariable, SessionVariable } from './dashboard/DashboardToolbar';
import { DashboardView } from './dashboard/DashboardView';
import { useDashboardQuery } from './dashboard/useDashboardQuery';
import { InspectDialog } from './dashboard/InspectDialog';
import { useDashboardShortcuts } from './dashboard/useShortcuts';
import { useDashboardState, type DashboardState } from './dashboardState';
import { problemText } from './time/labels';
import { TimeControls } from './time/TimeControls';
import { useDashboardTime } from './useDashboardTime';
import { useSessionOptions } from './useSessionOptions';

function dashboardFilters(state: DashboardState): DashboardQuery {
  return {
    agentVersions: state.versions,
    ...(state.tab === 'models' && state.model ? { model: state.model } : {}),
    ...(state.tab === 'tools' && state.tool ? { tool: state.tool } : {}),
  };
}

export function ObservabilityPage({ scope }: { scope: ObservabilityScope }) {
  const { msg } = useI18n();
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const [state, update] = useDashboardState();
  const time = useDashboardTime(state, update);
  const [inspected, setInspected] = useState<DashboardPanel | null>(null);
  const hoveredPanel = useRef('');
  const agentId = scope.kind === 'agent' ? scope.agentId : state.agentId;
  const queryScope = useMemo(
    () => ({
      orgUuid: orgUuid ?? '',
      workspaceId: activeWorkspaceId,
      agentId: agentId || undefined,
      sessionId: state.sessionId || undefined,
      ...time.timeWindow,
    }),
    [orgUuid, activeWorkspaceId, agentId, state.sessionId, time.timeWindow],
  );
  const sessionScope = useMemo(() => ({ ...queryScope, sessionId: undefined }), [queryScope]);
  const sessions = useSessionOptions(sessionScope);
  const filters = useMemo(() => dashboardFilters(state), [state]);
  const query = useDashboardQuery(time.timeWindow ? state.tab : null, queryScope, filters);
  const viewPanel = (id: string) => update({ viewPanel: id }, 'push');
  useDashboardShortcuts({
    shiftBack: () => time.shift(-1),
    shiftForward: () => time.shift(1),
    zoomOut: time.zoomOut,
    refresh: time.refresh,
    viewHovered: () => hoveredPanel.current && viewPanel(state.viewPanel ? '' : hoveredPanel.current),
    exitView: () => state.viewPanel && viewPanel(''),
  });
  const interactions = useMemo<DashboardInteractions>(
    () => ({
      syncId: `observability-${state.tab}`,
      timeZone: state.timeZone,
      viewPanel: state.viewPanel,
      fetching: query.isFetching,
      onZoom: time.zoomTo,
      onViewPanel: (id) => update({ viewPanel: id }, 'push'),
      onInspect: setInspected,
      onHoverPanel: (id) => {
        hoveredPanel.current = id;
      },
    }),
    [state.tab, state.timeZone, state.viewPanel, query.isFetching, time.zoomTo, update],
  );
  if (!orgUuid || !activeWorkspaceId)
    return (
      <p role="status" className="p-3 text-sm text-muted-foreground">
        {msg('observability.workspaceRequired', 'Select a workspace to view observability.')}
      </p>
    );
  const filterKind = state.tab === 'models' || state.tab === 'tools' ? state.tab : null;
  return (
    <TooltipProvider>
      <DashboardContext value={interactions}>
        <div className="flex h-full min-h-[500px] min-w-0 flex-col gap-4 px-3 py-3">
          <div className="min-w-0">
            {scope.kind === 'workspace' ? (
              <ResourcePageHeader title={msg('managedAgents.observability.title', 'Observability')} />
            ) : null}
            <DashboardToolbar
              workspaceId={activeWorkspaceId}
              agentId={agentId}
              fixedAgent={scope.kind === 'agent'}
              versions={state.versions}
              session={
                <SessionVariable
                  sessionId={state.sessionId}
                  options={sessions.options}
                  loading={sessions.loading}
                  onChange={(sessionId) => update({ sessionId })}
                />
              }
              filter={
                filterKind && (
                  <FilterVariable
                    kind={filterKind}
                    value={filterKind === 'models' ? state.model : state.tool}
                    options={query.data?.filter_options ?? []}
                    onChange={(value) => update(filterKind === 'models' ? { model: value } : { tool: value })}
                  />
                )
              }
              timeControls={
                <TimeControls
                  range={state.range}
                  now={time.anchor}
                  timeZone={state.timeZone}
                  refresh={state.refresh}
                  fetching={query.isFetching}
                  resolvedTitle={time.title}
                  onRangeChange={time.setRange}
                  onTimeZoneChange={(timeZone) => update({ timeZone })}
                  onShift={time.shift}
                  onZoomOut={time.zoomOut}
                  onRefresh={time.refresh}
                  onRefreshChange={(refresh) => update({ refresh })}
                />
              }
              onAgentChange={(next) => update({ agentId: next, versions: [], sessionId: '' })}
              onVersionsChange={(versions) => update({ versions })}
            />
          </div>
          {time.problem && (
            <p role="alert" className="text-sm text-destructive">
              {problemText(time.problem, msg)}
            </p>
          )}
          <Tabs
            value={state.tab}
            onValueChange={(value) => update({ tab: value as DashboardTab, viewPanel: '' })}
            className="flex min-w-0 flex-col gap-4"
          >
            <TabsList variant="line" className={state.viewPanel ? 'hidden' : 'gap-4'}>
              {DASHBOARD_TABS.map(([value, label]) => (
                <TabsTrigger key={value} value={value}>
                  {msg(`observability.tab.${value}`, label)}
                </TabsTrigger>
              ))}
            </TabsList>
            {DASHBOARD_TABS.map(([value]) => (
              <TabsContent key={value} value={value}>
                {state.tab === value && time.timeWindow && <DashboardView query={query} viewPanel={state.viewPanel} />}
              </TabsContent>
            ))}
          </Tabs>
          <InspectDialog panel={inspected} timeZone={state.timeZone} onClose={() => setInspected(null)} />
        </div>
      </DashboardContext>
    </TooltipProvider>
  );
}
