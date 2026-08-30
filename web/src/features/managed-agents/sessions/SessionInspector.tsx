import { Fragment, type ReactNode, useEffect, useMemo, useRef, useState } from 'react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Badge } from '../../../shared/ui/badge';
import { Button, buttonVariants } from '../../../shared/ui/button';
import {
  Combobox,
  ComboboxClear,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxGroupLabel,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxTrigger,
  ComboboxValue,
} from '../../../shared/ui/combobox';
import { InputGroup, InputGroupAddon, InputGroupInput } from '../../../shared/ui/input-group';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from '../../../shared/ui/resizable';
import { ScrollArea } from '../../../shared/ui/scroll-area';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { Separator } from '../../../shared/ui/separator';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../shared/ui/tooltip';
import { Ban, CheckCircle2, ChevronDown, CircleHelp, File, Plus, Search, X } from 'lucide-react';
import { retrieveAgent, retrieveFileMetadata, retrieveManagedEntity } from '../api';
import {
  type AgentApiResponse,
  type EnvironmentApiResponse,
  type QuickstartSessionEvent,
  type SessionApiResponse,
  type SessionDetailLane,
  type SessionEventListEntry,
  type SessionFileResourceFormValue,
  type VaultApiResponse,
} from '../types';
import {
  agentDetailHref,
  compactEntityId,
  errorMessage,
  handleInternalLinkClick,
  managedEntityDetailHref,
  objectRecord,
} from '../utils';
import {
  formatCompactTokenCount,
  formatSessionDuration,
  sessionEventEntryMatchesSelectedId,
  sessionEventThreadId,
  sessionStatusIsLive,
} from './sessionDetailModel';
import {
  buildInspectorCostPoints,
  buildInspectorEventRows,
  buildInspectorEventListItems,
  buildInspectorThreadRows,
  buildInspectorToolRows,
  buildInspectorToolTotals,
  filterInspectorEventRows,
  inspectorBackingEventIds,
  inspectorAgentEffort,
  inspectorAgentModel,
  inspectorEventFamily,
  inspectorEventSuffix,
  inspectorSessionListCost,
  sessionInspectorTabHref,
  SESSION_INSPECTOR_TABS,
  type InspectorContextPoint,
  type InspectorCostPoint,
  type InspectorSessionUsage,
  type InspectorThreadRow,
  type InspectorToolRow,
  type SessionInspectorTab,
} from './sessionInspectorModel';
import { EventDetailPanel } from './SessionTracePanel';
import { areSessionFileResourcesValid, SessionFileResourcesField } from './SessionFileResourcesField';
import { sessionEventProcessedTimestamp, sessionEventTimestamp } from './sessionTraceModel';

const SESSION_INSPECTOR_DETAIL_DEFAULT_HEIGHT = 360;
const SESSION_INSPECTOR_DETAIL_MIN_HEIGHT = 120;

export function SessionInspector({
  activeTab,
  activeLane,
  events,
  eventsByLaneId,
  lanes,
  onActiveTabChange,
  onAddFileResource = () => Promise.resolve(),
  onClose,
  onHoverEvent = () => {},
  onSelectEntry,
  onSelectLane,
  refreshKey,
  hoveredEventId = null,
  selectedEntry,
  session,
  workspaceId,
}: {
  activeTab: SessionInspectorTab;
  activeLane: string;
  events: QuickstartSessionEvent[];
  eventsByLaneId: Map<string, QuickstartSessionEvent[]>;
  lanes: SessionDetailLane[];
  onActiveTabChange: (tab: SessionInspectorTab) => void;
  onAddFileResource?: (resource: SessionFileResourceFormValue) => Promise<void>;
  onClose: () => void;
  onHoverEvent?: (eventId: string | null) => void;
  onSelectEntry: (entryId: string | null) => void;
  onSelectLane: (laneId: string) => void;
  refreshKey: number;
  hoveredEventId?: string | null;
  selectedEntry: SessionEventListEntry | null;
  session: SessionApiResponse;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const related = useSessionInspectorEntities(session, workspaceId, refreshKey);
  const agentReference = objectRecord(session.agent);
  const sessionAgentId = typeof agentReference.id === 'string' ? agentReference.id : '';
  const filenamesByFileId = useSessionInspectorFileMetadata(session, workspaceId);
  const tabsListRef = useRef<HTMLDivElement>(null);
  const [toolScope, setToolScope] = useState<InspectorToolScope>('all');
  const showToolScope = lanes.length > 1;
  useEffect(() => {
    if (!showToolScope) setToolScope('all');
  }, [showToolScope]);
  const activeToolLane = lanes.find((lane) => lane.id === activeLane);
  const scopedToolLanes = useMemo(() => {
    if (toolScope === 'all') return lanes;
    if (toolScope === 'thread') return activeToolLane ? [activeToolLane] : [];
    return activeToolLane ? lanes.filter((lane) => lane.group === activeToolLane.group) : [];
  }, [activeToolLane, lanes, toolScope]);
  const scopedToolEvents = useMemo(() => {
    if (toolScope === 'all') return events;
    const allowedEvents = new Set(scopedToolLanes.flatMap((lane) => eventsByLaneId.get(lane.id) ?? []));
    return events.filter((event) => allowedEvents.has(event));
  }, [events, eventsByLaneId, scopedToolLanes, toolScope]);
  const scopedToolAgent = toolScope === 'all' || scopedToolLanes.some((lane) => lane.isMain) ? related.agent : null;
  const toolRows = useMemo(
    () => buildInspectorToolRows(scopedToolEvents, scopedToolAgent),
    [scopedToolAgent, scopedToolEvents],
  );
  const threadRows = useMemo(
    () => buildInspectorThreadRows(lanes, eventsByLaneId, sessionStatusIsLive(session.status)),
    [eventsByLaneId, lanes, session.status],
  );
  useEffect(() => {
    const selectedTab = tabsListRef.current?.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]');
    selectedTab?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' });
  }, [activeTab]);
  return (
    <aside
      aria-label={msg('managedAgents.sessions.inspector.label', 'Session inspector')}
      className="session-inspector-shell flex h-full w-full min-h-0 min-w-0 flex-col overflow-hidden bg-card"
      data-testid="session-inspector"
      onKeyDown={(event) => {
        if (event.key !== 'Escape' || event.defaultPrevented) {
          return;
        }
        event.preventDefault();
        event.stopPropagation();
        if (selectedEntry) {
          onSelectEntry(null);
        } else {
          onClose();
        }
      }}
    >
      <Tabs
        value={activeTab}
        className="min-h-0 min-w-0 flex-1 gap-0"
        onValueChange={(value) => onActiveTabChange(value as SessionInspectorTab)}
      >
        <div className="flex h-8 flex-none items-center border-b border-border/60 pr-1">
          <TabsList
            ref={tabsListRef}
            aria-label={msg('managedAgents.sessions.inspector.label', 'Session inspector')}
            activateOnFocus={false}
            className="scrollbar-none h-8 min-w-0 flex-1 justify-start gap-0.5 overflow-x-auto overflow-y-hidden bg-transparent px-1 py-0"
          >
            {SESSION_INSPECTOR_TABS.map((tab) => (
              <TabsTrigger
                key={tab}
                value={tab}
                nativeButton={false}
                render={<a href={sessionInspectorTabHref(tab)} />}
                className="h-7 flex-none rounded-md px-2.5 text-[12px] font-medium text-muted-foreground after:hidden hover:bg-muted/40 hover:text-foreground focus-visible:border-transparent focus-visible:bg-muted/70 focus-visible:ring-0 focus-visible:outline-none data-active:bg-muted/70 data-active:text-foreground data-active:shadow-none group-data-[variant=default]/tabs-list:data-active:shadow-none dark:data-active:border-transparent dark:data-active:bg-muted/70"
                onClick={(event) => {
                  if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
                    event.preventBaseUIHandler();
                    return;
                  }
                  event.preventDefault();
                  if (tab === activeTab) onActiveTabChange(tab);
                }}
              >
                {inspectorTabLabel(tab, msg)}
              </TabsTrigger>
            ))}
          </TabsList>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={msg('managedAgents.sessions.inspector.close', 'Close inspector')}
                  data-inspector-close=""
                  className="size-7 rounded-md text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                  onClick={onClose}
                >
                  <X className="size-4" aria-hidden />
                </Button>
              }
            />
            <TooltipContent>{msg('managedAgents.sessions.inspector.close', 'Close inspector')}</TooltipContent>
          </Tooltip>
        </div>

        <TabsContent value="session" className="mt-0 min-h-0 min-w-0 overflow-hidden">
          <ScrollArea>
            <SessionOverviewPanel
              events={events}
              hoveredEventId={hoveredEventId}
              related={related}
              selectedEntry={selectedEntry}
              session={session}
              workspaceId={workspaceId}
              onHoverEvent={onHoverEvent}
              onSelectEntry={onSelectEntry}
            />
          </ScrollArea>
        </TabsContent>
        <TabsContent value="events" className="mt-0 min-h-0 min-w-0 overflow-hidden">
          <InspectorEventsPanel
            events={events}
            hoveredEventId={hoveredEventId}
            selectedEntry={selectedEntry}
            onHoverEvent={onHoverEvent}
            onSelectEntry={onSelectEntry}
          />
        </TabsContent>
        <TabsContent value="tools" className="mt-0 min-h-0 min-w-0 overflow-hidden">
          <InspectorToolsPanel
            events={scopedToolEvents}
            hoveredEventId={hoveredEventId}
            rows={toolRows}
            selectedEntry={selectedEntry}
            showThread={scopedToolLanes.length > 1}
            scope={toolScope}
            showScope={showToolScope}
            onHoverEvent={onHoverEvent}
            onSelectEntry={onSelectEntry}
            onScopeChange={setToolScope}
          />
        </TabsContent>
        <TabsContent value="resources" className="mt-0 min-h-0 min-w-0 overflow-hidden">
          <ScrollArea>
            <InspectorResourcesPanel
              filenamesByFileId={filenamesByFileId}
              session={session}
              workspaceId={workspaceId}
              onAddFileResource={onAddFileResource}
            />
          </ScrollArea>
        </TabsContent>
        <TabsContent value="threads" className="mt-0 min-h-0 min-w-0 overflow-hidden">
          <InspectorThreadsPanel
            activeLane={activeLane}
            agent={related.agent}
            agentId={sessionAgentId}
            hoveredEventId={hoveredEventId}
            rows={threadRows}
            workspaceId={workspaceId}
            onHoverEvent={onHoverEvent}
            onSelectLane={onSelectLane}
          />
        </TabsContent>
      </Tabs>
    </aside>
  );
}

type SessionInspectorEntities = {
  agent: AgentApiResponse | null;
  environment: EnvironmentApiResponse | null;
  vaults: VaultApiResponse[];
};

function SessionOverviewPanel({
  events,
  hoveredEventId,
  onHoverEvent,
  onSelectEntry,
  related,
  selectedEntry,
  session,
  workspaceId,
}: {
  events: QuickstartSessionEvent[];
  hoveredEventId: string | null;
  onHoverEvent: (eventId: string | null) => void;
  onSelectEntry: (entryId: string | null) => void;
  related: SessionInspectorEntities;
  selectedEntry: SessionEventListEntry | null;
  session: SessionApiResponse;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const agentReference = objectRecord(session.agent);
  const agentId = typeof agentReference.id === 'string' ? agentReference.id : '';
  const costPoints = useMemo(() => buildInspectorCostPoints(events), [events]);
  const latestCostPoint = costPoints.at(-1);
  const listCost = latestCostPoint
    ? { amount: latestCostPoint.cents / 100, currency: latestCostPoint.currency }
    : (inspectorSessionListCost(session.usage) ?? inspectorSessionListCost(session.stats));
  const selectedCostEventId =
    costPoints.find((point) => selectedEntry && sessionEventEntryMatchesSelectedId(selectedEntry, point.eventId))
      ?.eventId ?? null;
  const vaultIds = Array.isArray(session.vault_ids)
    ? session.vault_ids.filter((value): value is string => typeof value === 'string' && value.length > 0)
    : [];
  const rows: Array<[string, React.ReactNode]> = [
    [msg('common.id', 'ID'), <span className="font-mono">{compactEntityId(session.id)}</span>],
    [msg('common.status', 'Status'), <SessionStateBadge key="status" status={session.status} />],
    [msg('common.created', 'Created'), formatInspectorDate(session.created_at, formatters)],
    [msg('managedAgents.common.updatedAt', 'Updated'), formatInspectorDate(session.updated_at, formatters)],
    [
      msg('managedAgents.sessions.detail.agentTab', 'Agent'),
      agentId ? (
        <InspectorEntityLink href={agentDetailHref(workspaceId, agentId)}>
          {related.agent?.name || agentId}
        </InspectorEntityLink>
      ) : (
        '—'
      ),
    ],
    [
      msg('managedAgents.sessions.detail.environmentTab', 'Environment'),
      session.environment_id ? (
        <InspectorEntityLink href={managedEntityDetailHref(workspaceId, 'environments', session.environment_id)}>
          {related.environment?.name || session.environment_id}
        </InspectorEntityLink>
      ) : (
        '—'
      ),
    ],
    [
      msg('managedAgents.sessions.detail.vaultsTab', 'Vaults'),
      vaultIds.length ? (
        <span className="flex flex-wrap gap-x-2 gap-y-1">
          {vaultIds.map((vaultId) => {
            const vault = related.vaults.find((candidate) => candidate.id === vaultId);
            return (
              <InspectorEntityLink
                key={vaultId}
                href={managedEntityDetailHref(workspaceId, 'credential-vaults', vaultId)}
              >
                {vault?.display_name || vaultId}
              </InspectorEntityLink>
            );
          })}
        </span>
      ) : (
        '—'
      ),
    ],
    [msg('managedAgents.sessions.fields.deployment', 'Deployment'), session.deployment_id || '—'],
  ];
  return (
    <div className="px-5 py-4 text-[13px] leading-5">
      <h2 className="mb-4 text-sm font-semibold">{msg('managedAgents.sessions.inspector.session', 'Session')}</h2>
      <InspectorFacts rows={rows} />
      <Separator className="mt-5 bg-border/60" />
      <SessionCostSection
        firstEventAt={events[0] ? sessionEventProcessedTimestamp(events[0]) || sessionEventTimestamp(events[0]) : 0}
        hoveredEventId={hoveredEventId}
        listCost={listCost}
        points={costPoints}
        selectedEventId={selectedCostEventId}
        onHoverEvent={onHoverEvent}
        onSelectEntry={onSelectEntry}
      />
    </div>
  );
}

function SessionCostSection({
  firstEventAt,
  hoveredEventId,
  listCost,
  onHoverEvent,
  onSelectEntry,
  points,
  selectedEventId,
}: {
  firstEventAt: number;
  hoveredEventId: string | null;
  listCost: { amount: number; currency: string } | null;
  onHoverEvent: (eventId: string | null) => void;
  onSelectEntry: (entryId: string | null) => void;
  points: InspectorCostPoint[];
  selectedEventId: string | null;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const latestUsage = points.at(-1)?.usage;
  return (
    <section aria-label={msg('managedAgents.sessions.inspector.cost', 'Cost')} className="flex flex-col gap-3 pt-4">
      <div className="flex items-center justify-between gap-4 font-medium">
        <span>{msg('managedAgents.sessions.inspector.cost', 'Cost')}</span>
        <span className="font-mono tabular-nums">
          {listCost ? formatters.currency(listCost.amount, listCost.currency) : '—'}
        </span>
      </div>
      <SessionCostChart
        firstEventAt={firstEventAt}
        hoveredEventId={hoveredEventId}
        points={points}
        selectedEventId={selectedEventId}
        onHoverEvent={onHoverEvent}
        onSelectEntry={onSelectEntry}
      />
      {latestUsage ? <SessionUsageFacts usage={latestUsage} /> : null}
    </section>
  );
}

function SessionCostChart({
  firstEventAt,
  hoveredEventId,
  onHoverEvent,
  onSelectEntry,
  points,
  selectedEventId,
}: {
  firstEventAt: number;
  hoveredEventId: string | null;
  onHoverEvent: (eventId: string | null) => void;
  onSelectEntry: (entryId: string | null) => void;
  points: InspectorCostPoint[];
  selectedEventId: string | null;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const label = msg('managedAgents.sessions.inspector.cumulativeCost', 'Cumulative session cost over time');
  if (!points.length) {
    return (
      <div role="img" aria-label={label} className="grid h-[120px] place-items-center text-sm text-muted-foreground">
        {msg('managedAgents.sessions.inspector.noCostTracked', 'No cost tracked yet.')}
      </div>
    );
  }
  const firstPoint = points[0];
  const baselineAt = firstEventAt > 0 && firstEventAt <= firstPoint.at ? firstEventAt : undefined;
  const maxCents = Math.max(...points.map((point) => point.cents), 1);
  const currency = points.at(-1)?.currency ?? 'USD';
  const formatCost = (cents: number) => formatters.currency(cents / 100, currency);
  return (
    <InspectorStepChart
      ariaLabel={label}
      baselineAt={baselineAt}
      chartBottomInset={24}
      chartLeft={52}
      chartRightInset={8}
      chartTop={8}
      className="h-[120px] w-full"
      height={120}
      hoveredEventId={hoveredEventId}
      kind="cost"
      maxValue={maxCents}
      points={points.map((point) => ({
        at: point.at,
        eventId: point.eventId,
        title: `${formatCost(point.cents)} · ${msg('managedAgents.sessions.inspector.thisStep', 'This step')} ${formatCost(point.stepCents)}`,
        value: point.cents,
      }))}
      selectedEventId={selectedEventId}
      xTicks={[baselineAt ?? firstPoint.at, points.at(-1)?.at ?? firstPoint.at]}
      yTicks={[0, maxCents]}
      formatX={(at) => formatters.time(at, { hour: '2-digit', hour12: false, minute: '2-digit', second: '2-digit' })}
      formatY={formatCost}
      onHoverEvent={onHoverEvent}
      onSelectEntry={onSelectEntry}
    />
  );
}

type InspectorStepChartPoint = {
  at: number;
  eventId: string;
  title: string;
  value: number;
};

function InspectorStepChart({
  ariaLabel,
  baselineAt,
  chartBottomInset,
  chartLeft,
  chartRightInset,
  chartTop,
  className,
  formatX,
  formatY,
  height,
  hoveredEventId,
  kind,
  maxValue,
  onHoverEvent,
  onSelectEntry,
  points,
  selectedEventId,
  xTicks,
  yTicks,
}: {
  ariaLabel: string;
  baselineAt?: number;
  chartBottomInset: number;
  chartLeft: number;
  chartRightInset: number;
  chartTop: number;
  className: string;
  formatX: (value: number) => string;
  formatY: (value: number) => string;
  height: number;
  hoveredEventId: string | null;
  kind: 'context' | 'cost';
  maxValue: number;
  onHoverEvent: (eventId: string | null) => void;
  onSelectEntry?: (entryId: string | null) => void;
  points: InspectorStepChartPoint[];
  selectedEventId?: string | null;
  xTicks: number[];
  yTicks: number[];
}) {
  const width = 440;
  const chartRight = width - chartRightInset;
  const chartBottom = height - chartBottomInset;
  const pathPoints = baselineAt === undefined ? points : [{ at: baselineAt, value: 0 }, ...points];
  const minTime = Math.min(...pathPoints.map((point) => point.at));
  const maxTime = Math.max(...pathPoints.map((point) => point.at));
  const x = (at: number) => chartLeft + ((at - minTime) / Math.max(1, maxTime - minTime)) * (chartRight - chartLeft);
  const y = (value: number) => chartBottom - (value / maxValue) * (chartBottom - chartTop);
  const path = pathPoints.reduce((value, point, index) => {
    const pointPosition = `${x(point.at)} ${y(point.value)}`;
    return index === 0 ? `M ${pointPosition}` : `${value} H ${x(point.at)} V ${y(point.value)}`;
  }, '');
  const areaPath = `${path} L ${x(maxTime)} ${chartBottom} L ${x(minTime)} ${chartBottom} Z`;
  const activeEventId = points.some((point) => point.eventId === hoveredEventId) ? hoveredEventId : selectedEventId;
  return (
    <svg viewBox={`0 0 ${width} ${height}`} className={className} role="img" aria-label={ariaLabel}>
      {yTicks.map((value) => (
        <g key={value}>
          <line x1={chartLeft} x2={chartRight} y1={y(value)} y2={y(value)} stroke="var(--border)" />
          <text x={chartLeft - 7} y={y(value) + 4} textAnchor="end" fill="var(--muted-foreground)" fontSize="10">
            {formatY(value)}
          </text>
        </g>
      ))}
      <path d={areaPath} fill="var(--chart-1)" opacity="0.15" />
      <path d={path} fill="none" stroke="var(--chart-1)" strokeWidth="2" />
      {points.map((point) => {
        const active = activeEventId === point.eventId;
        return (
          <g
            key={point.eventId}
            role={onSelectEntry ? 'button' : undefined}
            tabIndex={onSelectEntry ? 0 : undefined}
            aria-label={onSelectEntry ? point.title : undefined}
            data-context-event-id={kind === 'context' ? point.eventId : undefined}
            data-cost-event-id={kind === 'cost' ? point.eventId : undefined}
            className="group/chart-point cursor-crosshair outline-none"
            onClick={() => onSelectEntry?.(point.eventId)}
            onPointerEnter={() => onHoverEvent(point.eventId)}
            onPointerLeave={() => onHoverEvent(null)}
            onKeyDown={(event) => {
              if (!onSelectEntry || (event.key !== 'Enter' && event.key !== ' ')) return;
              event.preventDefault();
              onSelectEntry(point.eventId);
            }}
          >
            <title>{point.title}</title>
            {active ? (
              <line
                x1={x(point.at)}
                x2={x(point.at)}
                y1={chartTop}
                y2={chartBottom}
                stroke="var(--ring)"
                strokeDasharray="2 2"
                opacity="0.5"
              />
            ) : null}
            <circle cx={x(point.at)} cy={y(point.value)} r="8" fill="transparent" />
            <circle
              cx={x(point.at)}
              cy={y(point.value)}
              r="3"
              fill="var(--chart-1)"
              stroke="var(--card)"
              strokeWidth="1.5"
              className={cn(
                'pointer-events-none opacity-0 transition-opacity group-hover/chart-point:opacity-100 group-focus/chart-point:opacity-100',
                active && 'opacity-100',
              )}
            />
          </g>
        );
      })}
      {xTicks.map((at, index) => (
        <text
          key={`${at}:${index}`}
          x={x(at)}
          y={height - 8}
          textAnchor={index === 0 ? 'start' : index === xTicks.length - 1 ? 'end' : 'middle'}
          fill="var(--muted-foreground)"
          fontSize="10"
          fontFamily="ui-monospace, SFMono-Regular, Menlo, monospace"
        >
          {formatX(at)}
        </text>
      ))}
    </svg>
  );
}

function SessionUsageFacts({ usage }: { usage: InspectorSessionUsage }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const rows: Array<[string, ReactNode]> = [
    [msg('managedAgents.observability.inputTokens', 'Input tokens'), formatters.number(usage.input)],
    [msg('managedAgents.observability.outputTokens', 'Output tokens'), formatters.number(usage.output)],
    [msg('managedAgents.sessions.trace.cacheRead', 'Cache read'), formatters.number(usage.cacheRead)],
    [msg('managedAgents.sessions.inspector.cacheWrite', 'Cache write'), formatters.number(usage.cacheWrite)],
  ];
  if (usage.webSearches > 0) {
    rows.push([
      msg('managedAgents.sessions.inspector.webSearches', 'Web searches'),
      formatters.number(usage.webSearches),
    ]);
  }
  rows.push([
    msg('managedAgents.observability.activeTime', 'Active time'),
    usage.activeSeconds === undefined ? '—' : formatSessionDuration(usage.activeSeconds * 1000, formatters, msg),
  ]);
  return (
    <div className="border-t border-border/60 pt-3">
      <div className="mb-2 flex items-center justify-between gap-3 text-xs font-medium">
        <span>{msg('analytics.usage.title', 'Usage')}</span>
        <span className="text-muted-foreground">
          {msg('managedAgents.sessions.inspector.sessionTotal', 'Session total')}
        </span>
      </div>
      <InspectorFacts rows={rows} />
    </div>
  );
}

function InspectorEventsPanel({
  events,
  hoveredEventId,
  onHoverEvent,
  onSelectEntry,
  selectedEntry,
}: {
  events: QuickstartSessionEvent[];
  hoveredEventId: string | null;
  onHoverEvent: (eventId: string | null) => void;
  onSelectEntry: (entryId: string | null) => void;
  selectedEntry: SessionEventListEntry | null;
}) {
  const rows = useMemo(() => buildInspectorEventRows(events), [events]);
  const wireTypes = useMemo(() => [...new Set(rows.map((row) => row.type))].sort(), [rows]);
  const [transcriptOnly, setTranscriptOnly] = useState(false);
  const [selectedTypes, setSelectedTypes] = useState<string[]>([]);
  const visibleRows = useMemo(
    () => filterInspectorEventRows(rows, { transcriptOnly, types: selectedTypes }),
    [rows, selectedTypes, transcriptOnly],
  );
  const visibleItems = useMemo(() => buildInspectorEventListItems(events, visibleRows), [events, visibleRows]);
  const backingEventIds = useMemo(() => inspectorBackingEventIds(events, selectedEntry), [events, selectedEntry]);
  const tabStopId =
    visibleRows.find((row) => selectedEntry && sessionEventEntryMatchesSelectedId(selectedEntry, row.id))?.id ??
    visibleRows[0]?.id;
  const rowIndexById = useMemo(() => new Map(visibleRows.map((row, index) => [row.id, index])), [visibleRows]);
  const formatters = useFormatters();
  const { msg } = useI18n();
  const eventRowsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!selectedEntry) return;
    const selectedRow = Array.from(eventRowsRef.current?.querySelectorAll<HTMLElement>('[data-event-id]') ?? []).find(
      (row) => row.dataset.eventId && sessionEventEntryMatchesSelectedId(selectedEntry, row.dataset.eventId),
    );
    selectedRow?.scrollIntoView?.({ block: 'nearest' });
  }, [selectedEntry, visibleRows]);
  const eventList = (
    <div className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden">
      <div className="flex flex-none items-center gap-1.5 px-3 py-2">
        <InspectorEventsFilter
          selectedTypes={selectedTypes}
          transcriptOnly={transcriptOnly}
          wireTypes={wireTypes}
          onSelectedTypesChange={setSelectedTypes}
          onTranscriptOnlyChange={setTranscriptOnly}
        />
      </div>
      <div className="min-h-0 flex-1 overflow-hidden">
        <ScrollArea
          data-testid="session-inspector-events-list"
          className="[&_[data-slot=scroll-area-content]]:!w-full [&_[data-slot=scroll-area-content]]:!max-w-full [&_[data-slot=scroll-area-content]]:!min-w-0"
        >
          <div
            ref={eventRowsRef}
            className="w-full max-w-full overflow-hidden px-3 pb-1 font-mono text-[12px] leading-[17px]"
          >
            <div
              data-testid="session-inspector-events-header"
              className="sticky top-0 z-10 flex h-6 items-center gap-1.5 border-b border-border/60 bg-card px-1 font-sans font-medium text-muted-foreground"
            >
              <span className="min-w-48 flex-none">{msg('managedAgents.sessions.inspector.event', 'Event')}</span>
              <span className="min-w-0 flex-1">{msg('managedAgents.sessions.inspector.preview', 'Preview')}</span>
              <span className="w-16 flex-none text-right">{msg('managedAgents.sessions.inspector.time', 'Time')}</span>
            </div>
            <div role="listbox" aria-label={msg('managedAgents.sessions.detail.eventsTab', 'Events')} className="pt-1">
              {visibleItems.map((item) => {
                const renderRow = (row: (typeof visibleRows)[number]) => {
                  const index = rowIndexById.get(row.id) ?? 0;
                  const selected = selectedEntry ? sessionEventEntryMatchesSelectedId(selectedEntry, row.id) : false;
                  const backed = backingEventIds.has(row.id);
                  const hovered = hoveredEventId === row.id;
                  return (
                    <button
                      key={row.id}
                      type="button"
                      role="option"
                      aria-selected={selected}
                      tabIndex={row.id === tabStopId ? 0 : -1}
                      data-event-id={row.id}
                      data-backed={backed || undefined}
                      data-hovered={hovered || undefined}
                      className={cn(
                        'session-inspector-event-row flex h-6 w-full min-w-0 items-center gap-1.5 rounded-sm px-1 text-left outline-none transition-colors hover:bg-accent/50 focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset',
                        selected && 'bg-accent text-accent-foreground',
                        !selected && hovered && 'bg-accent/50',
                        backed && 'font-semibold text-foreground',
                        !selected && !backed && 'text-foreground',
                      )}
                      onClick={() => onSelectEntry(row.id)}
                      onMouseEnter={() => onHoverEvent(row.id)}
                      onMouseLeave={() => onHoverEvent(null)}
                      onKeyDown={(event) => {
                        const targetIndex = inspectorEventKeyboardTargetIndex(event.key, index, visibleRows.length);
                        if (targetIndex === null) return;
                        event.preventDefault();
                        const target = visibleRows[targetIndex];
                        onSelectEntry(target.id);
                        const targetRow = Array.from(
                          eventRowsRef.current?.querySelectorAll<HTMLElement>('[data-event-id]') ?? [],
                        ).find((element) => element.dataset.eventId === target.id);
                        targetRow?.focus();
                        targetRow?.scrollIntoView?.({ block: 'nearest' });
                      }}
                    >
                      <span className="min-w-48 flex-none truncate">
                        <span className={inspectorEventFamilyTone(inspectorEventFamily(row.type))}>
                          {inspectorEventFamily(row.type)}
                        </span>
                        <span>{inspectorEventSuffix(row.type)}</span>
                      </span>
                      <span className="min-w-0 flex-1 truncate text-muted-foreground">{row.preview}</span>
                      <span className="w-16 flex-none text-right tabular-nums text-muted-foreground">
                        {row.processedAtMs
                          ? formatters.time(row.processedAtMs, { hour12: false, second: '2-digit' })
                          : 'queued'}
                      </span>
                    </button>
                  );
                };
                return item.type === 'turn' ? (
                  <div
                    key={item.id}
                    data-inspector-turn-group={item.id}
                    className="-mx-1 my-1.5 rounded-md border-[0.5px] border-foreground/20 p-[3.5px]"
                  >
                    {item.rows.map(renderRow)}
                  </div>
                ) : (
                  renderRow(item.row)
                );
              })}
              {!visibleRows.length ? (
                <p className="px-3 py-9 text-center font-sans text-xs text-muted-foreground">
                  {rows.length
                    ? msg('managedAgents.sessions.inspector.noMatchingEvents', 'No matching events.')
                    : msg('managedAgents.sessions.nested.noEvents', 'No events yet')}
                </p>
              ) : null}
            </div>
          </div>
        </ScrollArea>
      </div>
    </div>
  );
  if (!selectedEntry) {
    return eventList;
  }
  return (
    <InspectorListDetailSplit
      id="events"
      list={eventList}
      resizeLabel={msg('managedAgents.sessions.inspector.resizeEventDetail', 'Resize event detail')}
      detail={
        <div
          key={selectedEntry.id}
          data-testid="session-inspector-event-detail-content"
          className="session-inspector-detail-card relative z-20 flex h-full min-h-0 flex-col overflow-hidden bg-card"
        >
          <EventDetailPanel entry={selectedEntry} view="debug" placement="side" onClose={() => onSelectEntry(null)} />
        </div>
      }
    />
  );
}

function InspectorListDetailSplit({
  detail,
  id,
  list,
  resizeLabel,
}: {
  detail: ReactNode;
  id: string;
  list: ReactNode;
  resizeLabel: string;
}) {
  return (
    <ResizablePanelGroup
      id={`session-inspector-${id}-split`}
      orientation="vertical"
      className="h-full min-h-0 min-w-0 overflow-hidden"
    >
      <ResizablePanel
        id={`session-inspector-${id}-list-panel`}
        minSize={SESSION_INSPECTOR_DETAIL_MIN_HEIGHT}
        className="min-h-0"
      >
        {list}
      </ResizablePanel>
      <ResizableHandle aria-label={resizeLabel} className="-my-[2px] z-30 cursor-row-resize" />
      <ResizablePanel
        id={`session-inspector-${id}-detail-panel`}
        defaultSize={SESSION_INSPECTOR_DETAIL_DEFAULT_HEIGHT}
        minSize={SESSION_INSPECTOR_DETAIL_MIN_HEIGHT}
        groupResizeBehavior="preserve-pixel-size"
        className="min-h-0"
      >
        {detail}
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}

function InspectorEventsFilter({
  onSelectedTypesChange,
  onTranscriptOnlyChange,
  selectedTypes,
  transcriptOnly,
  wireTypes,
}: {
  onSelectedTypesChange: (types: string[]) => void;
  onTranscriptOnlyChange: (transcriptOnly: boolean) => void;
  selectedTypes: string[];
  transcriptOnly: boolean;
  wireTypes: string[];
}) {
  const { msg } = useI18n();
  const selectedCount = selectedTypes.length + Number(transcriptOnly);
  const selectedValues = transcriptOnly ? [TRANSCRIPT_FILTER_VALUE, ...selectedTypes] : selectedTypes;
  const items = [TRANSCRIPT_FILTER_VALUE, ...wireTypes];
  const label =
    selectedCount === 0
      ? msg('managedAgents.sessions.trace.allEvents', 'All events')
      : selectedCount === 1
        ? transcriptOnly
          ? msg('managedAgents.sessions.inspector.transcriptEvents', 'Transcript events')
          : selectedTypes[0]
        : msg('managedAgents.common.selectedCount', '{count} selected', { count: selectedCount });

  return (
    <Combobox
      multiple
      items={items}
      value={selectedValues}
      onValueChange={(values) => {
        onTranscriptOnlyChange(values.includes(TRANSCRIPT_FILTER_VALUE));
        onSelectedTypesChange(values.filter((value) => value !== TRANSCRIPT_FILTER_VALUE));
      }}
    >
      <ComboboxTrigger
        type="button"
        aria-label={msg('managedAgents.sessions.trace.filterEvents', 'Filter events')}
        className={cn(
          buttonVariants({ variant: 'outline', size: 'sm' }),
          'h-7 max-w-56 gap-1.5 px-2 text-xs font-normal shadow-none',
          selectedCount > 0 && 'border-ring/25 bg-accent/60',
        )}
      >
        <ComboboxValue>
          <span className="min-w-0 truncate">{label}</span>
        </ComboboxValue>
        <ChevronDown className="size-3.5 flex-none text-muted-foreground" aria-hidden />
      </ComboboxTrigger>
      <ComboboxContent align="start" sideOffset={6} className="w-80 p-0">
        <div className="p-1">
          <ComboboxInput
            showTrigger={false}
            aria-label={msg('managedAgents.sessions.inspector.searchEventTypes', 'Search event types')}
            placeholder={msg('managedAgents.sessions.inspector.search', 'Search')}
            className="h-7 shadow-none"
          />
        </div>
        <ComboboxList className="max-h-64 border-t border-border/60">
          <ComboboxEmpty>
            {msg('managedAgents.sessions.inspector.noMatchingEventTypes', 'No matching event types.')}
          </ComboboxEmpty>
          <ComboboxItem value={TRANSCRIPT_FILTER_VALUE}>
            {msg('managedAgents.sessions.inspector.transcriptEvents', 'Transcript events')}
          </ComboboxItem>
          <ComboboxGroup className="border-t border-border/60">
            <ComboboxGroupLabel>{msg('managedAgents.sessions.inspector.eventTypes', 'Event types')}</ComboboxGroupLabel>
            {wireTypes.map((type) => (
              <ComboboxItem key={type} value={type}>
                <span className="truncate font-mono text-[12px]">{type}</span>
              </ComboboxItem>
            ))}
          </ComboboxGroup>
        </ComboboxList>
        <ComboboxClear
          render={
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="m-1 w-[calc(100%-0.5rem)] justify-center text-xs font-medium"
            />
          }
        >
          {msg('managedAgents.sessions.trace.clearFilters', 'Clear filters')}
        </ComboboxClear>
      </ComboboxContent>
    </Combobox>
  );
}

const TRANSCRIPT_FILTER_VALUE = 'TRANSCRIPT';

function inspectorEventKeyboardTargetIndex(key: string, index: number, length: number) {
  if (!length) return null;
  if (key === 'ArrowDown' || key === 'j') return Math.min(index + 1, length - 1);
  if (key === 'ArrowUp' || key === 'k') return Math.max(index - 1, 0);
  if (key === 'Home') return 0;
  if (key === 'End') return length - 1;
  return null;
}

type InspectorToolScope = 'all' | 'agent' | 'thread';

function InspectorToolsPanel({
  events,
  hoveredEventId,
  onHoverEvent,
  onSelectEntry,
  onScopeChange,
  rows,
  selectedEntry,
  showScope,
  showThread,
  scope,
}: {
  events: QuickstartSessionEvent[];
  hoveredEventId: string | null;
  onHoverEvent: (eventId: string | null) => void;
  onSelectEntry: (entryId: string | null) => void;
  onScopeChange: (scope: InspectorToolScope) => void;
  rows: InspectorToolRow[];
  selectedEntry: SessionEventListEntry | null;
  showScope: boolean;
  showThread: boolean;
  scope: InspectorToolScope;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [query, setQuery] = useState('');
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const totals = useMemo(() => buildInspectorToolTotals(rows), [rows]);
  const selectedEventIds = useMemo(() => inspectorBackingEventIds(events, selectedEntry), [events, selectedEntry]);
  const selected = rows.find((row) => row.key === selectedKey);
  useEffect(() => {
    if (selectedKey && !selected) setSelectedKey(null);
  }, [selected, selectedKey]);
  const groupLabel = (row: InspectorToolRow) =>
    row.kind === 'built-in'
      ? msg('managedAgents.agents.detail.builtInTools', 'Built-in tools')
      : row.kind === 'custom'
        ? msg('managedAgents.agents.detail.customTools', 'Custom tools')
        : row.kind === 'unconfigured'
          ? msg('managedAgents.sessions.inspector.calledNotConfigured', 'Called, not configured')
          : row.group;
  const normalizedQuery = query.trim().toLowerCase();
  const visibleRows = normalizedQuery
    ? rows.filter((row) =>
        `${row.name} ${groupLabel(row)} ${row.permission} ${row.configuredOn ?? ''}`
          .toLowerCase()
          .includes(normalizedQuery),
      )
    : rows;
  const toolList = (
    <div className="flex h-full min-h-0 flex-col overflow-hidden text-sm">
      <div className="flex min-h-10 flex-none items-center gap-2 px-4 py-1.5">
        <InputGroup className="h-7 w-48 max-w-full shadow-none">
          <InputGroupAddon className="pl-2 pr-1.5">
            <Search className="size-3.5" aria-hidden />
          </InputGroupAddon>
          <InputGroupInput
            type="search"
            aria-label={msg('managedAgents.sessions.inspector.filterTools', 'Filter tools')}
            placeholder={msg('managedAgents.sessions.inspector.filterTools', 'Filter tools')}
            value={query}
            className="text-xs"
            onChange={(event) => setQuery(event.target.value)}
          />
        </InputGroup>
        {showScope ? (
          <Select<InspectorToolScope> value={scope} onValueChange={(value) => value && onScopeChange(value)}>
            <SelectTrigger
              size="sm"
              aria-label={msg('managedAgents.sessions.inspector.scope', 'Scope')}
              className="max-w-40 text-xs"
            >
              <SelectValue>
                {scope === 'all'
                  ? msg('managedAgents.sessions.inspector.allThreads', 'All threads')
                  : scope === 'agent'
                    ? msg('managedAgents.sessions.inspector.currentAgent', 'Current agent')
                    : msg('managedAgents.sessions.inspector.currentThread', 'Current thread')}
              </SelectValue>
            </SelectTrigger>
            <SelectContent align="start" alignItemWithTrigger={false}>
              <SelectItem value="all" label={msg('managedAgents.sessions.inspector.allThreads', 'All threads')}>
                {msg('managedAgents.sessions.inspector.allThreads', 'All threads')}
              </SelectItem>
              <SelectItem value="agent" label={msg('managedAgents.sessions.inspector.currentAgent', 'Current agent')}>
                {msg('managedAgents.sessions.inspector.currentAgent', 'Current agent')}
              </SelectItem>
              <SelectItem
                value="thread"
                label={msg('managedAgents.sessions.inspector.currentThread', 'Current thread')}
              >
                {msg('managedAgents.sessions.inspector.currentThread', 'Current thread')}
              </SelectItem>
            </SelectContent>
          </Select>
        ) : null}
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div className="px-4 pb-1 [&_[data-slot=table-container]]:overflow-visible">
          <Table className="table-fixed text-[11px]">
            <TableHeader className="sticky top-0 z-10 bg-card">
              <TableRow className="hover:bg-transparent">
                <TableHead className="h-6 min-w-24 px-1.5">{msg('common.name', 'Name')}</TableHead>
                <TableHead className="h-6 w-[88px] px-1.5">
                  {msg('managedAgents.sessions.inspector.permission', 'Permission')}
                </TableHead>
                <TableHead className="h-6 w-14 px-1.5 text-right">
                  {msg('managedAgents.sessions.inspector.calls', 'Calls')}
                </TableHead>
                <TableHead className="h-6 w-[72px] px-1.5 text-right">
                  {msg('managedAgents.sessions.inspector.failed', 'Failed')}
                </TableHead>
                <TableHead className="h-6 w-16 px-1.5 text-right">p50</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleRows.map((row, index) => (
                <Fragment key={row.key}>
                  {visibleRows[index - 1]?.group !== row.group ? (
                    <TableRow className="border-b-0 hover:bg-transparent" data-tool-group={row.kind}>
                      <TableCell
                        colSpan={5}
                        className="h-7 px-1.5 pb-0 pt-2 text-[10px] font-medium text-muted-foreground"
                      >
                        {groupLabel(row)}
                      </TableCell>
                    </TableRow>
                  ) : null}
                  <TableRow
                    tabIndex={0}
                    data-state={row.key === selectedKey ? 'selected' : undefined}
                    className={cn(
                      'cursor-pointer outline-none focus-visible:shadow-[inset_0_0_0_1px_var(--ring)]',
                      !row.calls.length && 'text-muted-foreground',
                    )}
                    onClick={() => setSelectedKey(row.key)}
                    onKeyDown={(event) => {
                      if (event.key !== 'Enter' && event.key !== ' ') return;
                      event.preventDefault();
                      setSelectedKey(row.key);
                    }}
                  >
                    <TableCell className="h-6 min-w-0 truncate px-1.5 py-0 font-mono">{row.name}</TableCell>
                    <TableCell className="h-6 min-w-0 truncate px-1.5 py-0">
                      <ToolPermission permission={row.permission} />
                    </TableCell>
                    <TableCell className="h-6 min-w-0 truncate px-1.5 py-0 text-right font-mono tabular-nums">
                      {row.calls.length}
                    </TableCell>
                    <TableCell
                      className={cn(
                        'h-6 min-w-0 truncate px-1.5 py-0 text-right font-mono tabular-nums',
                        row.failed > 0 ? 'text-destructive' : row.calls.length > 0 ? 'text-muted-foreground' : '',
                      )}
                    >
                      {row.calls.length ? row.failed : '—'}
                    </TableCell>
                    <TableCell className="h-6 min-w-0 truncate px-1.5 py-0 text-right font-mono tabular-nums">
                      {row.p50Ms === undefined ? '—' : formatSessionDuration(row.p50Ms, formatters, msg)}
                    </TableCell>
                  </TableRow>
                </Fragment>
              ))}
            </TableBody>
          </Table>
          {!visibleRows.length ? (
            <p className="px-3 py-9 text-center text-xs text-muted-foreground">
              {msg('managedAgents.sessions.inspector.noMatchingTools', 'No matching tools.')}
            </p>
          ) : null}
        </div>
      </ScrollArea>
    </div>
  );
  if (!rows.length) return toolList;
  return (
    <div
      className="h-full min-h-0 overflow-hidden"
      onKeyDown={(event) => {
        if (event.key !== 'Escape' || !selectedKey) return;
        event.preventDefault();
        event.stopPropagation();
        setSelectedKey(null);
      }}
    >
      <InspectorListDetailSplit
        id="tools"
        list={toolList}
        resizeLabel={msg('managedAgents.sessions.inspector.resizeToolDetail', 'Resize tool detail')}
        detail={
          selected ? (
            <InspectorToolDetail
              hoveredEventId={hoveredEventId}
              onClose={() => setSelectedKey(null)}
              onHoverEvent={onHoverEvent}
              onSelectEntry={onSelectEntry}
              row={selected}
              selectedEventIds={selectedEventIds}
              showThread={showThread}
            />
          ) : (
            <InspectorToolsOverview totals={totals} />
          )
        }
      />
    </div>
  );
}

function InspectorToolsOverview({ totals }: { totals: ReturnType<typeof buildInspectorToolTotals> }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const noneLabel = msg('managedAgents.sessions.inspector.none', 'none');
  const failedRate = totals.failed === totals.calls ? 1 : Math.min(Math.max(totals.failed / totals.calls, 0.01), 0.99);
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-card">
      <InspectorDetailHeading
        name={msg('managedAgents.sessions.inspector.overview', 'Overview')}
        summary={`${totals.tools} tools · ${totals.used} used · ${totals.calls} calls`}
      />
      <ScrollArea>
        <div className="px-4 py-3 text-sm">
          <div className="flex items-center gap-4">
            <ToolOutcomeRing totals={totals} />
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs">
              <dt className="flex items-center gap-1.5 text-muted-foreground">
                <span className="inline-block size-2 rounded-full bg-destructive" aria-hidden />
                {msg('managedAgents.sessions.inspector.failed', 'Failed')}
              </dt>
              <dd className={cn('font-mono tabular-nums', totals.failed > 0 && 'text-destructive')}>
                {totals.failed > 0
                  ? `${formatters.number(totals.failed)} (${formatters.number(failedRate, {
                      style: 'percent',
                      maximumFractionDigits: 0,
                    })})`
                  : noneLabel}
              </dd>
              <dt className="flex items-center gap-1.5 text-muted-foreground">
                <span className="inline-block size-2 rounded-full bg-chart-4" aria-hidden />
                {msg('managedAgents.sessions.inspector.denied', 'Denied')}
              </dt>
              <dd className="font-mono tabular-nums">
                {totals.denied > 0 ? formatters.number(totals.denied) : noneLabel}
              </dd>
              <dt className="flex items-center gap-1.5 text-muted-foreground">
                <span className="inline-block size-2 rounded-full bg-chart-2" aria-hidden />
                {msg('managedAgents.sessions.inspector.completed', 'Completed')}
              </dt>
              <dd className="font-mono tabular-nums">{formatters.number(totals.completed)}</dd>
              {totals.inFlight > 0 ? (
                <>
                  <dt className="flex items-center gap-1.5 text-muted-foreground">
                    <span className="inline-block size-2 rounded-full border border-foreground/30" aria-hidden />
                    {msg('managedAgents.sessions.inspector.inFlight', 'In flight')}
                  </dt>
                  <dd className="font-mono tabular-nums">{formatters.number(totals.inFlight)}</dd>
                </>
              ) : null}
            </dl>
          </div>
          <Separator className="mt-5" />
          <dl className="grid grid-cols-[1fr_auto] gap-y-2 pt-4">
            <dt className="text-muted-foreground">
              {msg('managedAgents.sessions.inspector.timeInTools', 'Time in tools · Total')}
            </dt>
            <dd className="font-mono tabular-nums">{formatSessionDuration(totals.timeInToolsMs, formatters, msg)}</dd>
            <dt className="text-muted-foreground">{msg('managedAgents.sessions.inspector.executing', 'Executing')}</dt>
            <dd className="font-mono tabular-nums">{formatSessionDuration(totals.executingMs, formatters, msg)}</dd>
            <dt className="text-muted-foreground">
              {msg('managedAgents.sessions.inspector.waiting', 'Waiting on you')}
            </dt>
            <dd className="font-mono tabular-nums">{formatSessionDuration(totals.waitingMs, formatters, msg)}</dd>
          </dl>
        </div>
      </ScrollArea>
    </div>
  );
}

function InspectorToolDetail({
  hoveredEventId,
  onClose,
  onHoverEvent,
  onSelectEntry,
  row,
  selectedEventIds,
  showThread,
}: {
  hoveredEventId: string | null;
  onClose: () => void;
  onHoverEvent: (eventId: string | null) => void;
  onSelectEntry: (entryId: string | null) => void;
  row: InspectorToolRow;
  selectedEventIds: Set<string>;
  showThread: boolean;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const showWaited = row.calls.some((call) => call.confirmationEvent);
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-card">
      <InspectorDetailHeading
        lead={msg('managedAgents.sessions.inspector.tool', 'Tool')}
        name={row.name}
        action={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('managedAgents.sessions.inspector.closeToolDetail', 'Close tool detail')}
            className="size-7"
            onClick={onClose}
          >
            <X className="size-3.5" aria-hidden />
          </Button>
        }
      />
      <ScrollArea>
        <div className="px-4 py-3 text-sm">
          <InspectorFacts
            rows={[
              [
                msg('managedAgents.sessions.inspector.permission', 'Permission'),
                <ToolPermission key="permission" permission={row.permission} />,
              ],
              [msg('managedAgents.sessions.inspector.calls', 'Calls'), row.calls.length],
              [msg('managedAgents.sessions.inspector.failed', 'Failed'), row.failed],
              [
                row.kind !== 'unconfigured'
                  ? msg('managedAgents.sessions.inspector.configuredOn', 'Configured on')
                  : msg('managedAgents.sessions.inspector.source', 'Source'),
                row.configuredOn ||
                  msg('managedAgents.sessions.inspector.calledNotConfigured', 'Called, not configured'),
              ],
              ['p50', row.p50Ms === undefined ? '—' : formatSessionDuration(row.p50Ms, formatters, msg)],
            ]}
          />
          <Separator className="my-3" />
          <Table
            className={cn(
              'table-fixed text-[11px]',
              showWaited && showThread ? 'min-w-[600px]' : showWaited || showThread ? 'min-w-[510px]' : 'min-w-[420px]',
            )}
          >
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="h-6 w-20 px-1.5">
                  {msg('managedAgents.sessions.inspector.time', 'Time')}
                </TableHead>
                <TableHead className="h-6 px-1.5">{msg('managedAgents.sessions.inspector.input', 'Input')}</TableHead>
                <TableHead className="h-6 w-20 px-1.5">{msg('common.status', 'Status')}</TableHead>
                <TableHead className="h-6 w-[76px] px-1.5 text-right">
                  {msg('managedAgents.sessions.inspector.duration', 'Duration')}
                </TableHead>
                {showWaited ? (
                  <TableHead className="h-6 w-20 px-1.5 text-right">
                    {msg('managedAgents.sessions.inspector.waited', 'Waited')}
                  </TableHead>
                ) : null}
                {showThread ? (
                  <TableHead className="h-6 w-24 px-1.5">
                    {msg('managedAgents.sessions.inspector.thread', 'Thread')}
                  </TableHead>
                ) : null}
              </TableRow>
            </TableHeader>
            <TableBody>
              {row.calls.map((call) => {
                const calledAt = sessionEventProcessedTimestamp(call.event) || sessionEventTimestamp(call.event);
                const confirmedAt = call.confirmationEvent
                  ? sessionEventProcessedTimestamp(call.confirmationEvent) ||
                    sessionEventTimestamp(call.confirmationEvent)
                  : 0;
                const waitedMs = confirmedAt && calledAt ? Math.max(0, confirmedAt - calledAt) : undefined;
                const threadId = sessionEventThreadId(call.event);
                const selected = selectedEventIds.has(call.rawEventId);
                const hovered = sessionEventEntryMatchesSelectedId(call, hoveredEventId);
                return (
                  <TableRow
                    key={call.id}
                    tabIndex={0}
                    data-state={selected ? 'selected' : undefined}
                    className={cn(
                      'cursor-pointer outline-none focus-visible:shadow-[inset_0_0_0_1px_var(--ring)]',
                      hovered && !selected && 'bg-muted/50',
                    )}
                    onClick={() => onSelectEntry(call.rawEventId)}
                    onKeyDown={(event) => {
                      if (event.key !== 'Enter' && event.key !== ' ') return;
                      event.preventDefault();
                      onSelectEntry(call.rawEventId);
                    }}
                    onPointerEnter={() => onHoverEvent(call.rawEventId)}
                    onPointerLeave={() => onHoverEvent(null)}
                  >
                    <TableCell className="h-6 truncate px-1.5 py-0 font-mono tabular-nums">
                      {calledAt ? formatters.time(calledAt, { hour12: false, second: '2-digit' }) : '—'}
                    </TableCell>
                    <TableCell className="h-6 min-w-0 truncate px-1.5 py-0">{call.inputPreview || '—'}</TableCell>
                    <TableCell className="h-6 truncate px-1.5 py-0 capitalize">
                      {call.lifecycle.replace('_', ' ')}
                    </TableCell>
                    <TableCell className="h-6 truncate px-1.5 py-0 text-right font-mono tabular-nums">
                      {call.executionMs ? formatSessionDuration(call.executionMs, formatters, msg) : '—'}
                    </TableCell>
                    {showWaited ? (
                      <TableCell className="h-6 truncate px-1.5 py-0 text-right font-mono tabular-nums">
                        {waitedMs === undefined ? '—' : formatSessionDuration(waitedMs, formatters, msg)}
                      </TableCell>
                    ) : null}
                    {showThread ? (
                      <TableCell className="h-6 truncate px-1.5 py-0 font-mono text-muted-foreground">
                        {threadId ? compactEntityId(threadId) : '—'}
                      </TableCell>
                    ) : null}
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      </ScrollArea>
    </div>
  );
}

function InspectorDetailHeading({
  action,
  lead,
  name,
  summary,
}: {
  action?: ReactNode;
  lead?: ReactNode;
  name: ReactNode;
  summary?: ReactNode;
}) {
  return (
    <div className="flex h-8 flex-none items-center gap-1 border-b border-border/60 px-3 text-xs">
      {lead ? <span className="text-muted-foreground">{lead}</span> : null}
      <strong className="min-w-0 flex-1 truncate font-medium">{name}</strong>
      {summary ? <span className="truncate text-muted-foreground">{summary}</span> : null}
      {action}
    </div>
  );
}

function InspectorResourcesPanel({
  filenamesByFileId,
  onAddFileResource,
  session,
  workspaceId,
}: {
  filenamesByFileId: Record<string, { name: string; size: number }>;
  onAddFileResource: (resource: SessionFileResourceFormValue) => Promise<void>;
  session: SessionApiResponse;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const [query, setQuery] = useState('');
  const [dialogOpen, setDialogOpen] = useState(false);
  const [draftResources, setDraftResources] = useState<SessionFileResourceFormValue[]>([]);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const canSave = draftResources.length === 1 && areSessionFileResourcesValid(draftResources);
  const resources = session.resources.filter((resource) => {
    const file = resource.file_id ? filenamesByFileId[resource.file_id] : undefined;
    const value = `${resource.mount_path ?? ''} ${file?.name ?? ''} ${resource.file_id ?? ''}`.toLowerCase();
    return value.includes(query.trim().toLowerCase());
  });
  return (
    <div className="px-4 pb-3 pt-2 [&_[data-slot=table-container]]:overflow-visible">
      <div className="mb-2 flex items-center justify-between gap-3">
        <InputGroup className="h-7 w-48 max-w-full min-w-0 shadow-none">
          <InputGroupAddon className="pl-2 pr-1.5">
            <Search className="size-3.5" aria-hidden />
          </InputGroupAddon>
          <InputGroupInput
            type="search"
            aria-label={msg('managedAgents.sessions.inspector.filterResources', 'Filter resources')}
            placeholder={msg('managedAgents.sessions.inspector.filterResources', 'Filter resources')}
            value={query}
            className="text-xs"
            onChange={(event) => setQuery(event.target.value)}
          />
        </InputGroup>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button type="button" variant="outline" size="sm">
                <Plus aria-hidden />
                {msg('managedAgents.common.resource', 'Resource')}
                <ChevronDown aria-hidden />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onClick={() => {
                setDraftResources([{ fileId: '', mountPath: '' }]);
                setSaveError(null);
                setDialogOpen(true);
              }}
            >
              <File aria-hidden />
              {msg('managedAgents.sessions.resources.typeFile', 'File')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <Table className="table-fixed text-xs">
        <TableHeader className="sticky top-0 z-10 bg-card">
          <TableRow className="hover:bg-transparent">
            <TableHead className="h-6 px-1.5">{msg('managedAgents.sessions.inspector.path', 'Path')}</TableHead>
            <TableHead className="h-6 w-24 px-1.5 text-right">
              {msg('managedAgents.sessions.inspector.size', 'Size')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {resources.map((resource, index) => {
            const file = resource.file_id ? filenamesByFileId[resource.file_id] : undefined;
            const path = resource.mount_path || file?.name || resource.file_id || '—';
            return (
              <TableRow key={resource.id ?? resource.file_id ?? index}>
                <TableCell className="h-8 min-w-0 truncate px-1.5 py-0 font-mono" title={path}>
                  <span className="flex min-w-0 items-center gap-1.5">
                    <File className="size-3.5 flex-none text-muted-foreground" aria-hidden />
                    <span className="truncate">{path}</span>
                  </span>
                </TableCell>
                <TableCell className="h-8 min-w-0 truncate px-1.5 py-0 text-right font-mono text-muted-foreground">
                  {file ? formatters.bytes(file.size) : '—'}
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      {!resources.length ? (
        <p className="px-3 py-9 text-center text-xs text-muted-foreground">
          {msg('managedAgents.sessions.nested.noResources', 'No resources mounted')}
        </p>
      ) : null}
      <Dialog open={dialogOpen} onOpenChange={(open) => !open && !saving && setDialogOpen(false)}>
        <DialogContent className="flex max-h-[min(720px,calc(100dvh-2rem))] flex-col sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>{msg('managedAgents.sessions.resources.add', 'Add resource')}</DialogTitle>
            <DialogDescription>
              {msg('managedAgents.sessions.resources.description', 'Mount files into the session uploads directory.')}
            </DialogDescription>
          </DialogHeader>
          <form
            className="flex min-h-0 flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (saving || !canSave) return;
              setSaving(true);
              setSaveError(null);
              void onAddFileResource(draftResources[0]!)
                .then(() => setDialogOpen(false))
                .catch((error) => setSaveError(errorMessage(error)))
                .finally(() => setSaving(false));
            }}
          >
            <div className="min-h-0 overflow-y-auto px-px">
              <SessionFileResourcesField
                resources={draftResources}
                showAddButton={false}
                workspaceId={workspaceId}
                onChange={setDraftResources}
              />
            </div>
            {saveError ? <p className="text-sm text-destructive">{saveError}</p> : null}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={saving} onClick={() => setDialogOpen(false)}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button type="submit" disabled={saving || !canSave}>
                {saving ? msg('common.saving', 'Saving...') : msg('common.add', 'Add')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function InspectorThreadsPanel({
  activeLane,
  agent,
  agentId,
  hoveredEventId,
  onHoverEvent,
  onSelectLane,
  rows,
  workspaceId,
}: {
  activeLane: string;
  agent: AgentApiResponse | null;
  agentId: string;
  hoveredEventId: string | null;
  onHoverEvent: (eventId: string | null) => void;
  onSelectLane: (laneId: string) => void;
  rows: InspectorThreadRow[];
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const selected = rows.find((row) => row.id === activeLane) ?? rows[0];
  const threadList = (
    <ScrollArea>
      <div className="px-4 pb-1 pt-1 text-sm [&_[data-slot=table-container]]:overflow-visible">
        <Table className="table-fixed text-[11px]">
          <TableHeader className="sticky top-0 z-10 bg-card">
            <TableRow className="hover:bg-transparent">
              <TableHead className="h-6 px-1.5">{msg('managedAgents.sessions.inspector.thread', 'Thread')}</TableHead>
              <TableHead className="h-6 w-24 px-1.5">{msg('common.status', 'Status')}</TableHead>
              <TableHead className="h-6 w-[72px] px-1.5 text-right">
                {msg('managedAgents.sessions.inspector.context', 'Context')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow
                key={row.id}
                data-state={row.id === selected?.id ? 'selected' : undefined}
                tabIndex={0}
                className="cursor-pointer outline-none focus-visible:shadow-[inset_0_0_0_1px_var(--ring)]"
                onClick={() => onSelectLane(row.id)}
                onKeyDown={(event) => {
                  if (event.key !== 'Enter' && event.key !== ' ') return;
                  event.preventDefault();
                  onSelectLane(row.id);
                }}
              >
                <TableCell className="h-6 min-w-0 truncate px-1.5 py-0 font-medium text-primary">{row.label}</TableCell>
                <TableCell className="h-6 min-w-0 truncate px-1.5 py-0">
                  <SessionStateBadge status={row.status} />
                </TableCell>
                <TableCell className="h-6 min-w-0 truncate px-1.5 py-0 text-right font-mono tabular-nums">
                  {row.context ? formatCompactTokenCount(row.context, formatters) : '—'}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </ScrollArea>
  );
  if (!selected) return threadList;
  const selectedAgent = selected.isMain ? agent : null;
  const selectedAgentId = selected.isMain ? agentId : '';
  return (
    <InspectorListDetailSplit
      id="threads"
      list={threadList}
      resizeLabel={msg('managedAgents.sessions.inspector.resizeThreadDetail', 'Resize thread detail')}
      detail={
        <div className="flex h-full min-h-0 flex-col overflow-hidden bg-card">
          <InspectorDetailHeading
            lead={msg('managedAgents.sessions.inspector.thread', 'Thread')}
            name={selected.label}
          />
          <ScrollArea>
            <div className="px-4 py-3 text-sm">
              <InspectorFacts
                rows={[
                  [
                    msg('managedAgents.sessions.detail.agentTab', 'Agent'),
                    selectedAgentId ? (
                      <span className="inline-flex flex-wrap items-baseline gap-x-1.5">
                        <InspectorEntityLink href={agentDetailHref(workspaceId, selectedAgentId)}>
                          {selectedAgent?.name || selectedAgentId}
                        </InspectorEntityLink>
                        {selectedAgent ? <span className="text-muted-foreground">v{selectedAgent.version}</span> : null}
                      </span>
                    ) : (
                      '—'
                    ),
                  ],
                  [
                    msg('analytics.table.model', 'Model'),
                    selectedAgent ? <span className="font-mono">{inspectorAgentModel(selectedAgent)}</span> : '—',
                  ],
                  [
                    msg('managedAgents.sessions.inspector.effort', 'Effort'),
                    selectedAgent ? inspectorAgentEffort(selectedAgent) : '—',
                  ],
                ]}
              />
              <div className="mt-5">
                <div className="mb-2 flex items-center justify-between gap-4 font-semibold">
                  <span>{msg('managedAgents.sessions.inspector.contextUsage', 'Context usage')}</span>
                  <span className="font-mono">
                    {selected.context ? formatCompactTokenCount(selected.context, formatters) : '—'}
                  </span>
                </div>
                <ContextUsageChart
                  hoveredEventId={hoveredEventId}
                  points={selected.contextPoints}
                  onHoverEvent={onHoverEvent}
                />
              </div>
            </div>
          </ScrollArea>
        </div>
      }
    />
  );
}

function InspectorFacts({ rows }: { rows: Array<[string, React.ReactNode]> }) {
  return (
    <dl className="grid grid-cols-[8.5rem_minmax(0,1fr)] gap-x-3 gap-y-2">
      {rows.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="text-muted-foreground">{label}</dt>
          <dd className="min-w-0 break-words">{value ?? '—'}</dd>
        </div>
      ))}
    </dl>
  );
}

function SessionStateBadge({ status }: { status: string }) {
  const { msg } = useI18n();
  const normalized = status.toLowerCase();
  const running = normalized === 'running';
  const rescheduling = normalized === 'rescheduling' || normalized === 'rescheduled';
  const label =
    normalized === 'running'
      ? msg('managedAgents.sessions.statusRunning', 'Running')
      : rescheduling
        ? msg('managedAgents.sessions.statusRescheduling', 'Rescheduling')
        : normalized === 'idle'
          ? msg('managedAgents.sessions.statusIdle', 'Idle')
          : normalized === 'terminated'
            ? msg('managedAgents.sessions.statusTerminated', 'Terminated')
            : status.charAt(0).toUpperCase() + status.slice(1);
  return (
    <Badge
      variant="secondary"
      className={running ? 'bg-success-bg text-success' : rescheduling ? 'bg-warning-bg text-warning' : ''}
    >
      {label}
    </Badge>
  );
}

function ToolPermission({ permission }: { permission: InspectorToolRow['permission'] }) {
  const { msg } = useI18n();
  if (permission === 'unknown') return <span className="text-muted-foreground">—</span>;
  if (permission === 'mixed') {
    return <span className="text-muted-foreground">{msg('managedAgents.sessions.inspector.mixed', 'Mixed')}</span>;
  }
  if (permission === 'deny') {
    return (
      <span className="inline-flex items-center gap-1 text-destructive">
        <Ban className="size-3.5" aria-hidden />
        {msg('managedAgents.agents.detail.alwaysDeny', 'Always deny')}
      </span>
    );
  }
  if (permission === 'ask') {
    return (
      <span className="inline-flex items-center gap-1 text-warning">
        <CircleHelp className="size-3.5" aria-hidden />
        {msg('managedAgents.agents.detail.alwaysAsk', 'Always ask')}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 text-success">
      <CheckCircle2 className="size-3.5" aria-hidden />
      {msg('managedAgents.agents.detail.alwaysAllow', 'Always allow')}
    </span>
  );
}

function ToolOutcomeRing({ totals }: { totals: ReturnType<typeof buildInspectorToolTotals> }) {
  const { msg } = useI18n();
  const total = totals.completed + totals.failed + totals.denied + totals.inFlight;
  const failedEnd = total ? (totals.failed / total) * 100 : 0;
  const deniedEnd = total ? failedEnd + (totals.denied / total) * 100 : 0;
  const completedEnd = total ? deniedEnd + (totals.completed / total) * 100 : 0;
  const background = total
    ? `conic-gradient(from -90deg, var(--destructive) 0 ${failedEnd}%, var(--warning) ${failedEnd}% ${deniedEnd}%, var(--success) ${deniedEnd}% ${completedEnd}%, var(--muted) ${completedEnd}% 100%)`
    : 'var(--muted)';
  return (
    <div
      role="img"
      aria-label={msg(
        'managedAgents.sessions.inspector.toolOutcomeSummary',
        '{completed} completed, {failed} failed, {denied} denied, {inFlight} in flight',
        totals,
      )}
      className="grid size-16 flex-none place-items-center rounded-full"
      style={{ background }}
    >
      <span className="size-[50px] rounded-full bg-card" aria-hidden />
    </div>
  );
}

function ContextUsageChart({
  hoveredEventId,
  onHoverEvent,
  points,
}: {
  hoveredEventId: string | null;
  onHoverEvent: (eventId: string | null) => void;
  points: InspectorContextPoint[];
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  if (!points.length) {
    return (
      <div className="grid h-[140px] place-items-center text-sm text-muted-foreground">
        {msg('managedAgents.sessions.inspector.noModelRequests', 'No model requests yet.')}
      </div>
    );
  }
  const minTime = points[0]?.at ?? 0;
  const maxTime = points.at(-1)?.at ?? minTime + 1;
  const maxTokens = Math.max(...points.map((point) => point.tokens), 1);
  const scaleMax = Math.max(40_000, Math.ceil(maxTokens / 40_000) * 40_000);
  const yTicks = [0, scaleMax / 3, (scaleMax * 2) / 3, scaleMax];
  const durationMs = maxTime - minTime;
  const xTicks = durationMs > 0 ? [minTime, minTime + durationMs / 2, maxTime] : [minTime];
  return (
    <InspectorStepChart
      ariaLabel={msg('managedAgents.sessions.inspector.contextChartLabel', 'Context size at each model request')}
      chartBottomInset={30}
      chartLeft={48}
      chartRightInset={12}
      chartTop={12}
      className="h-[140px] w-full"
      formatX={(at) =>
        formatters.time(at, {
          hour: '2-digit',
          hour12: false,
          minute: '2-digit',
          ...(durationMs < 120_000 ? { second: '2-digit' } : {}),
        })
      }
      formatY={(tokens) => formatCompactTokenCount(tokens, formatters)}
      height={140}
      hoveredEventId={hoveredEventId}
      kind="context"
      maxValue={scaleMax}
      points={points.map((point) => ({
        at: point.at,
        eventId: point.eventId,
        title: `${formatters.time(point.at, {
          hour: '2-digit',
          hour12: false,
          minute: '2-digit',
          second: '2-digit',
        })} · ${formatCompactTokenCount(point.tokens, formatters)}`,
        value: point.tokens,
      }))}
      xTicks={xTicks}
      yTicks={yTicks}
      onHoverEvent={onHoverEvent}
    />
  );
}

function useSessionInspectorEntities(session: SessionApiResponse, workspaceId: string, refreshKey: number) {
  const [related, setRelated] = useState<SessionInspectorEntities>({ agent: null, environment: null, vaults: [] });
  useEffect(() => {
    let active = true;
    const reference = objectRecord(session.agent);
    const agentId = typeof reference.id === 'string' ? reference.id : '';
    const version = typeof reference.version === 'number' ? reference.version : null;
    const vaultIds = Array.isArray(session.vault_ids)
      ? session.vault_ids.filter((value): value is string => typeof value === 'string' && value.length > 0)
      : [];
    void Promise.allSettled([
      agentId ? retrieveAgent(agentId, workspaceId, version) : Promise.resolve(null),
      session.environment_id
        ? retrieveManagedEntity('environments', session.environment_id, workspaceId)
        : Promise.resolve(null),
      Promise.allSettled(vaultIds.map((vaultId) => retrieveManagedEntity('credential-vaults', vaultId, workspaceId))),
    ]).then((results) => {
      if (!active) return;
      const vaultResults = results[2].status === 'fulfilled' ? results[2].value : [];
      setRelated({
        agent: results[0].status === 'fulfilled' ? (results[0].value as AgentApiResponse | null) : null,
        environment: results[1].status === 'fulfilled' ? (results[1].value as EnvironmentApiResponse | null) : null,
        vaults: vaultResults.flatMap((result) =>
          result.status === 'fulfilled' ? [result.value as VaultApiResponse] : [],
        ),
      });
    });
    return () => {
      active = false;
    };
  }, [refreshKey, session.agent, session.environment_id, session.vault_ids, workspaceId]);
  return related;
}

function useSessionInspectorFileMetadata(session: SessionApiResponse, workspaceId: string) {
  const [files, setFiles] = useState<Record<string, { name: string; size: number }>>({});
  const fileIds = useMemo(
    () => [...new Set(session.resources.map((resource) => resource.file_id).filter(Boolean))] as string[],
    [session.resources],
  );
  useEffect(() => {
    let active = true;
    void Promise.allSettled(fileIds.map((id) => retrieveFileMetadata(id, workspaceId))).then((results) => {
      if (!active) return;
      setFiles(
        Object.fromEntries(
          results.flatMap((result) =>
            result.status === 'fulfilled'
              ? [[result.value.id, { name: result.value.filename, size: result.value.size_bytes }] as const]
              : [],
          ),
        ),
      );
    });
    return () => {
      active = false;
    };
  }, [fileIds, workspaceId]);
  return files;
}

function inspectorTabLabel(tab: SessionInspectorTab, msg: ReturnType<typeof useI18n>['msg']) {
  const labels: Record<SessionInspectorTab, string> = {
    session: msg('managedAgents.sessions.inspector.session', 'Session'),
    events: msg('managedAgents.sessions.detail.eventsTab', 'Events'),
    tools: msg('managedAgents.sessions.inspector.tools', 'Tools'),
    resources: msg('managedAgents.sessions.detail.resourcesTab', 'Resources'),
    threads: msg('managedAgents.sessions.inspector.threads', 'Threads'),
  };
  return labels[tab];
}

function inspectorEventFamilyTone(family: string) {
  if (family === 'agent') return 'text-session-speaker-agent';
  if (family === 'span') return 'text-session-event-span';
  if (family === 'session') return 'text-muted-foreground';
  if (family === 'user') return 'text-session-speaker-user';
  return 'text-foreground';
}

function formatInspectorDate(value: string, formatters: ReturnType<typeof useFormatters>) {
  return formatters.date(value, {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function InspectorEntityLink({ children, href }: { children: React.ReactNode; href: string }) {
  return (
    <a
      href={href}
      className="inline-block max-w-full truncate rounded-[2px] align-bottom font-medium text-session-link underline decoration-current/45 underline-offset-4 transition-[color,text-decoration-color] hover:decoration-current focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:ring-offset-2 focus-visible:ring-offset-background"
      onClick={(event) => handleInternalLinkClick(event, href)}
    >
      {children}
    </a>
  );
}
