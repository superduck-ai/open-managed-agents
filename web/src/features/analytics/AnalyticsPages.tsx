import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { ExternalLink } from 'lucide-react';
export { UsagePage, CostPage } from './MeteredAnalyticsPage';
import { AnalyticsPageRoot, FilterBar, FilterControl, MetricPanel } from './analytics-chrome';
import { ChevronDown, Clock3, Gauge, Logs, RefreshCw, Search, Server, SlidersHorizontal } from 'lucide-react';
import { useState } from 'react';
import { cn } from '@/shared/lib/utils';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from '@/shared/ui/card';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/shared/ui/dropdown-menu';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/shared/ui/empty';
import { Input } from '@/shared/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { useI18n } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { useWorkspace } from '../../shared/workspaces/context';

type FilterOption = { value: string; label: string };

const modelRows = [
  { name: 'Opus 4.8', rpm: '4,000', itpm: '400,000', otpm: '80,000' },
  { name: 'Sonnet 4.6', rpm: '4,000', itpm: '400,000', otpm: '80,000' },
  { name: 'Haiku 4.5', rpm: '4,000', itpm: '400,000', otpm: '80,000' },
];

const analyticsModelOptions = modelRows.map((row) => ({
  value: row.name.toLowerCase().replace(/[^a-z0-9]+/g, '-'),
  label: row.name,
}));

const linesPerPageOptions: FilterOption[] = [
  { value: '10', label: '10' },
  { value: '25', label: '25' },
  { value: '50', label: '50' },
  { value: '100', label: '100' },
];

export function CachingPage() {
  const { msg } = useI18n();
  const workspaceLabel = useWorkspaceLabel();
  const workspaceOptions: FilterOption[] = [
    { value: 'default', label: workspaceLabel },
    { value: 'all', label: msg('common.all', 'All') },
  ];
  const modelOptions: FilterOption[] = [{ value: 'all', label: msg('common.all', 'All') }, ...analyticsModelOptions];
  const rangeOptions: FilterOption[] = [
    { value: 'last-7-days', label: msg('analytics.filter.last7Days', 'Last 7 days') },
    { value: 'last-30-days', label: msg('analytics.filter.last30Days', 'Last 30 days') },
    { value: 'month-to-date', label: msg('analytics.filter.monthToDate', 'Month to date') },
  ];
  const groupByOptions: FilterOption[] = [
    { value: 'model', label: msg('analytics.table.model', 'Model') },
    { value: 'workspace', label: msg('analytics.filter.workspace', 'Workspace') },
  ];
  const [filters, setFilters] = useState({
    workspace: 'default',
    model: 'all',
    range: 'last-7-days',
    groupBy: 'model',
  });
  const cachingBody = msg(
    'analytics.caching.body',
    'Add {code} to eligible prompt blocks to reuse common context and reduce input costs.',
    { code: 'cache_control' },
  );
  const [cachingBodyBeforeCode, ...cachingBodyAfterCode] = cachingBody.split('cache_control');

  return (
    <AnalyticsPageRoot title={msg('analytics.caching.title', 'Caching')} icon={Gauge}>
      <FilterBar>
        <FilterControl
          label={msg('analytics.filter.workspace', 'Workspace')}
          value={filters.workspace}
          options={workspaceOptions}
          onValueChange={(workspace) => setFilters((current) => ({ ...current, workspace }))}
        />
        <FilterControl
          label={msg('analytics.filter.model', 'Model')}
          value={filters.model}
          options={modelOptions}
          onValueChange={(model) => setFilters((current) => ({ ...current, model }))}
        />
        <FilterControl
          label={msg('analytics.filter.range', 'Range')}
          value={filters.range}
          options={rangeOptions}
          onValueChange={(range) => setFilters((current) => ({ ...current, range }))}
        />
        <FilterControl
          label={msg('analytics.filter.groupBy', 'Group by')}
          value={filters.groupBy}
          options={groupByOptions}
          onValueChange={(groupBy) => setFilters((current) => ({ ...current, groupBy }))}
        />
      </FilterBar>

      <Card className="rounded-lg p-0">
        <Empty className="min-h-[390px] rounded-lg border-0 px-6 py-16">
          <EmptyHeader className="max-w-[560px]">
            <EmptyMedia
              variant="icon"
              className="size-12 rounded-full border border-border bg-secondary text-foreground"
            >
              <Server className="size-5" aria-hidden />
            </EmptyMedia>
            <EmptyTitle>
              <h2 className="text-[22px] font-semibold leading-8 text-foreground">
                {msg('analytics.caching.emptyTitle', "You're not using prompt caching")}
              </h2>
            </EmptyTitle>
            <EmptyDescription className="max-w-[560px] text-muted-foreground">
              {cachingBodyBeforeCode}
              <code className="rounded bg-secondary px-1.5 py-0.5 text-foreground">cache_control</code>
              {cachingBodyAfterCode.join('cache_control')}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent className="max-w-[560px]">
            <ButtonLink
              href="https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching"
              variant="outline"
              size="lg"
              className="mt-5"
              target="_blank"
              rel="noreferrer"
            >
              {msg('common.learnMore', 'Learn more')}
              <ExternalLink className="size-3.5" aria-hidden />
            </ButtonLink>
            <p className="mt-4 text-xs leading-5 text-muted-foreground/70">
              {msg(
                'analytics.caching.alreadyCaching',
                'Already caching? It can take a few minutes for new usage to appear.',
              )}
            </p>
          </EmptyContent>
        </Empty>
      </Card>
    </AnalyticsPageRoot>
  );
}

export function RateLimitsPage() {
  const { msg } = useI18n();
  const workspaceLabel = useWorkspaceLabel();
  const workspaceOptions: FilterOption[] = [
    { value: 'default', label: workspaceLabel },
    { value: 'all', label: msg('common.all', 'All') },
  ];
  const modelOptions: FilterOption[] = [{ value: 'all', label: msg('common.all', 'All') }, ...analyticsModelOptions];
  const [filters, setFilters] = useState({
    workspace: 'default',
    model: 'all',
  });
  const [hasRefreshed, setHasRefreshed] = useState(false);

  return (
    <AnalyticsPageRoot title={msg('analytics.rateLimits.title', 'Rate limits')} icon={Gauge}>
      <FilterBar>
        <FilterControl
          label={msg('analytics.filter.workspace', 'Workspace')}
          value={filters.workspace}
          options={workspaceOptions}
          onValueChange={(workspace) => setFilters((current) => ({ ...current, workspace }))}
        />
        <FilterControl
          label={msg('analytics.filter.model', 'Model')}
          value={filters.model}
          options={modelOptions}
          onValueChange={(model) => setFilters((current) => ({ ...current, model }))}
        />
      </FilterBar>

      <div className="grid gap-3 md:grid-cols-3">
        <MetricPanel
          title={msg('analytics.rateLimits.requestsPerMinute', 'Requests per Minute')}
          value="0%"
          detail="0 of 4,000 used"
        />
        <MetricPanel
          title={msg('analytics.rateLimits.inputTokensPerMinute', 'Input Tokens per Minute')}
          value="0%"
          detail="0 of 400,000 used"
        />
        <MetricPanel
          title={msg('analytics.rateLimits.outputTokensPerMinute', 'Output Tokens per Minute')}
          value="0%"
          detail="0 of 80,000 used"
        />
      </div>

      <Card className="gap-0 rounded-lg p-0">
        <CardHeader className="border-b border-border px-5 py-4">
          <div>
            <CardTitle className="text-sm font-semibold text-foreground">
              {msg('analytics.rateLimits.modelLimits', 'Model limits')}
            </CardTitle>
            <CardDescription className="mt-1 text-sm text-muted-foreground">
              {msg('analytics.rateLimits.currentUsage', 'Current minute usage across requests and tokens.')}
              {hasRefreshed ? (
                <span className="mt-1 block text-xs text-muted-foreground/70" aria-live="polite">
                  {msg('analytics.rateLimits.refreshed', 'Updated just now.')}
                </span>
              ) : null}
            </CardDescription>
          </div>
          <CardAction>
            <Button
              type="button"
              variant="outline"
              size="lg"
              onClick={() => {
                setHasRefreshed(true);
              }}
            >
              <RefreshCw className="size-4" aria-hidden />
              {msg('common.refresh', 'Refresh')}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent className="p-0">
          <Table className="min-w-full text-left">
            <TableHeader className="text-xs text-muted-foreground/70">
              <TableRow className="border-b border-border hover:bg-transparent">
                <TableHead className="px-5 py-3 text-muted-foreground/70">
                  {msg('analytics.table.model', 'Model')}
                </TableHead>
                <TableHead className="px-5 py-3 text-muted-foreground/70">
                  {msg('analytics.table.requests', 'Requests')}
                </TableHead>
                <TableHead className="px-5 py-3 text-muted-foreground/70">
                  {msg('analytics.table.inputTokens', 'Input Tokens')}
                </TableHead>
                <TableHead className="px-5 py-3 text-muted-foreground/70">
                  {msg('analytics.table.outputTokens', 'Output Tokens')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {modelRows.map((row) => (
                <TableRow key={row.name} className="border-b border-border text-foreground last:border-0">
                  <TableCell className="px-5 py-4 font-medium">{row.name}</TableCell>
                  <LimitCell limit={row.rpm} />
                  <LimitCell limit={row.itpm} />
                  <LimitCell limit={row.otpm} />
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </AnalyticsPageRoot>
  );
}

export function LogsPage() {
  const { msg } = useI18n();
  const routeWorkspaceId = getWorkspaceIdFromPath();
  const { workspaces } = useWorkspace();
  const allWorkspacesLabel = msg('workspace.all', 'All workspaces');
  const workspaceOptions: FilterOption[] = routeWorkspaceId
    ? [{ value: routeWorkspaceId, label: workspaces.find((w) => w.id === routeWorkspaceId)?.name ?? routeWorkspaceId }]
    : [
        { value: 'all-workspaces', label: allWorkspacesLabel },
        ...workspaces.map((w) => ({ value: w.id, label: w.name })),
      ];
  const modelOptions: FilterOption[] = [{ value: 'all', label: msg('common.all', 'All') }, ...analyticsModelOptions];
  const serviceAccountOptions: FilterOption[] = [
    { value: 'all', label: msg('common.all', 'All') },
    { value: 'batch-runner', label: 'Batch runner' },
    { value: 'ops-bot', label: 'Ops bot' },
  ];
  const requestTypeOptions: FilterOption[] = [
    { value: 'messages', label: msg('analytics.logs.typeMessages', 'Messages') },
    { value: 'batches', label: msg('analytics.logs.typeBatches', 'Batches') },
    { value: 'files', label: msg('analytics.logs.typeFiles', 'Files') },
    { value: 'managed-agents', label: msg('analytics.logs.typeManagedAgents', 'Managed agents') },
  ];
  const serviceTierFilterOptions: FilterOption[] = [
    { value: 'standard', label: msg('analytics.logs.serviceTierStandard', 'Standard') },
    { value: 'priority', label: msg('analytics.logs.serviceTierPriority', 'Priority') },
    { value: 'batch', label: msg('analytics.logs.serviceTierBatch', 'Batch') },
  ];
  const rangeOptions: FilterOption[] = [
    { value: 'last-24-hours', label: msg('analytics.filter.last24Hours', 'Last 24 hours') },
    { value: 'last-7-days', label: msg('analytics.filter.last7Days', 'Last 7 days') },
    { value: 'last-30-days', label: msg('analytics.filter.last30Days', 'Last 30 days') },
  ];
  const [filters, setFilters] = useState({
    workspace: routeWorkspaceId ?? 'all-workspaces',
    model: 'all',
    serviceAccount: 'all',
    range: 'last-24-hours',
    requestTypes: [] as string[],
    serviceTiers: [] as string[],
    linesPerPage: '10',
  });
  const hasAdvancedFilters = filters.requestTypes.length > 0 || filters.serviceTiers.length > 0;
  const clearAdvancedFilters = () => {
    setFilters((current) => ({ ...current, requestTypes: [], serviceTiers: [] }));
  };

  return (
    <AnalyticsPageRoot title={msg('analytics.logs.title', 'Logs')} icon={Logs}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <FilterBar compact>
          <FilterControl
            label={msg('analytics.filter.workspace', 'Workspace')}
            value={routeWorkspaceId ?? filters.workspace}
            options={workspaceOptions}
            disabled={Boolean(routeWorkspaceId)}
            onValueChange={(workspace) => setFilters((current) => ({ ...current, workspace }))}
          />
          <FilterControl
            label={msg('analytics.filter.model', 'Model')}
            value={filters.model}
            options={modelOptions}
            onValueChange={(model) => setFilters((current) => ({ ...current, model }))}
          />
          <FilterControl
            label={msg('analytics.filter.serviceAccount', 'Service account')}
            value={filters.serviceAccount}
            options={serviceAccountOptions}
            onValueChange={(serviceAccount) => setFilters((current) => ({ ...current, serviceAccount }))}
          />
          <FilterControl
            label={msg('analytics.filter.range', 'Range')}
            value={filters.range}
            options={rangeOptions}
            onValueChange={(range) => setFilters((current) => ({ ...current, range }))}
          />
        </FilterBar>
        <div className="flex items-center gap-2 text-xs text-muted-foreground/70">
          <Clock3 className="size-3.5" aria-hidden />
          <span>June 18, 2026 at 11:34 PM GMT+8</span>
        </div>
      </div>

      <Card className="gap-0 overflow-hidden rounded-lg p-0">
        <CardHeader className="border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <Search className="size-4 text-muted-foreground/70" aria-hidden />
            <Input
              type="search"
              aria-label={msg('analytics.logs.searchAria', 'Search logs')}
              placeholder={msg('analytics.logs.searchPlaceholder', 'Search request IDs')}
              className="h-8 flex-1 border-0 bg-transparent px-0 text-sm shadow-none focus-visible:border-transparent"
            />
            <LogsFiltersMenu
              requestTypeOptions={requestTypeOptions}
              selectedRequestTypes={filters.requestTypes}
              serviceTierOptions={serviceTierFilterOptions}
              selectedServiceTiers={filters.serviceTiers}
              onRequestTypesChange={(requestTypes) => setFilters((current) => ({ ...current, requestTypes }))}
              onServiceTiersChange={(serviceTiers) => setFilters((current) => ({ ...current, serviceTiers }))}
              onClear={clearAdvancedFilters}
            />
          </div>
        </CardHeader>

        <CardContent className="p-0">
          <Table className="min-w-[980px] text-left">
            <TableHeader className="text-xs text-muted-foreground/70">
              <TableRow className="border-b border-border hover:bg-transparent">
                {[
                  msg('analytics.table.time', 'Time'),
                  msg('common.id', 'ID'),
                  msg('analytics.table.model', 'Model'),
                  msg('analytics.table.inputTokens', 'Input Tokens'),
                  msg('analytics.table.outputTokens', 'Output Tokens'),
                  msg('analytics.table.type', 'Type'),
                  msg('analytics.table.serviceTier', 'Service Tier'),
                  msg('analytics.table.request', 'Request'),
                ].map((heading) => (
                  <TableHead key={heading} className="px-4 py-3 text-muted-foreground/70">
                    {heading}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              <TableRow>
                <TableCell colSpan={8} className="h-[260px] px-4 text-center">
                  <div className="mx-auto flex max-w-[360px] flex-col items-center">
                    <Logs className="size-8 text-muted-foreground/70" aria-hidden />
                    <div className="mt-4 text-sm font-medium text-foreground">
                      {hasAdvancedFilters
                        ? msg('analytics.logs.filteredEmptyTitle', 'No logs match the current filters')
                        : msg('analytics.logs.emptyTitle', 'No logs found')}
                    </div>
                    <p className="mt-2 text-sm leading-6 text-muted-foreground">
                      {hasAdvancedFilters
                        ? msg(
                            'analytics.logs.filteredEmptyBody',
                            'Try clearing filters or broadening the selected range.',
                          )
                        : msg(
                            'analytics.logs.emptyBody',
                            'Requests will appear here after API traffic is sent from this workspace.',
                          )}
                    </p>
                    {hasAdvancedFilters ? (
                      <Button type="button" variant="outline" size="sm" className="mt-4" onClick={clearAdvancedFilters}>
                        {msg('analytics.logs.clearFilters', 'Clear filters')}
                      </Button>
                    ) : null}
                  </div>
                </TableCell>
              </TableRow>
            </TableBody>
          </Table>

          <div className="flex items-center justify-between border-t border-border px-4 py-3 text-sm text-muted-foreground">
            <div className="flex items-center gap-2">
              <span>{msg('analytics.logs.linesPerPage', 'Lines per page')}</span>
              <Select<string>
                value={filters.linesPerPage}
                items={linesPerPageOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    setFilters((current) => ({ ...current, linesPerPage: nextValue }));
                  }
                }}
              >
                <SelectTrigger
                  aria-label={msg('analytics.logs.linesPerPage', 'Lines per page')}
                  size="sm"
                  className="border-border bg-background text-foreground shadow-sm"
                >
                  <SelectValue>{filters.linesPerPage}</SelectValue>
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {linesPerPageOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>0-0 of 0</div>
          </div>
        </CardContent>
      </Card>
    </AnalyticsPageRoot>
  );
}

function LogsFiltersMenu({
  requestTypeOptions,
  selectedRequestTypes,
  serviceTierOptions,
  selectedServiceTiers,
  onRequestTypesChange,
  onServiceTiersChange,
  onClear,
}: {
  requestTypeOptions: FilterOption[];
  selectedRequestTypes: string[];
  serviceTierOptions: FilterOption[];
  selectedServiceTiers: string[];
  onRequestTypesChange: (value: string[]) => void;
  onServiceTiersChange: (value: string[]) => void;
  onClear: () => void;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const activeFilterCount = selectedRequestTypes.length + selectedServiceTiers.length;
  const selectedRequestTypeSet = new Set(selectedRequestTypes);
  const selectedServiceTierSet = new Set(selectedServiceTiers);

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-label={
              activeFilterCount > 0
                ? msg('analytics.logs.filtersActive', 'Filters, {count} active', { count: activeFilterCount })
                : msg('common.filters', 'Filters')
            }
            className={cn(
              'gap-2',
              activeFilterCount > 0 && 'border-primary/30 bg-primary/5 text-foreground hover:bg-primary/10',
            )}
          />
        }
      >
        <SlidersHorizontal className="size-3.5" aria-hidden />
        {msg('common.filters', 'Filters')}
        {activeFilterCount > 0 ? (
          <span className="inline-flex min-w-5 items-center justify-center rounded-full bg-primary/10 px-1.5 py-0.5 text-[11px] font-semibold text-foreground">
            {activeFilterCount}
          </span>
        ) : null}
        <ChevronDown className="size-3.5 text-muted-foreground/70" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={8} className="w-72 bg-popover">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{msg('analytics.table.type', 'Type')}</DropdownMenuLabel>
          {requestTypeOptions.map((option) => {
            const checked = selectedRequestTypeSet.has(option.value);
            return (
              <DropdownMenuCheckboxItem
                key={option.value}
                checked={checked}
                className="h-8 px-2 text-sm text-foreground"
                onCheckedChange={() => {
                  onRequestTypesChange(toggleMultiSelectFilterValue(selectedRequestTypes, option.value, checked));
                }}
              >
                {option.label}
              </DropdownMenuCheckboxItem>
            );
          })}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLabel>{msg('analytics.table.serviceTier', 'Service Tier')}</DropdownMenuLabel>
          {serviceTierOptions.map((option) => {
            const checked = selectedServiceTierSet.has(option.value);
            return (
              <DropdownMenuCheckboxItem
                key={option.value}
                checked={checked}
                className="h-8 px-2 text-sm text-foreground"
                onCheckedChange={() => {
                  onServiceTiersChange(toggleMultiSelectFilterValue(selectedServiceTiers, option.value, checked));
                }}
              >
                {option.label}
              </DropdownMenuCheckboxItem>
            );
          })}
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={activeFilterCount === 0} className="h-8 px-2 text-sm font-medium" onClick={onClear}>
          {msg('analytics.logs.clearFilters', 'Clear filters')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function toggleMultiSelectFilterValue(selectedValues: string[], option: string, checked: boolean) {
  return checked ? selectedValues.filter((item) => item !== option) : [...selectedValues, option];
}

function LimitCell({ limit }: { limit: string }) {
  return (
    <TableCell className="px-5 py-4">
      <div className="flex items-center gap-3">
        <div className="h-1.5 w-24 overflow-hidden rounded-full bg-secondary">
          <div className="h-full w-0 bg-accent" />
        </div>
        <span className="text-muted-foreground">0 / {limit}</span>
      </div>
    </TableCell>
  );
}

function useWorkspaceLabel(routeWorkspaceId?: string | null) {
  const { activeWorkspace, workspaces } = useWorkspace();
  const workspaceId = routeWorkspaceId || activeWorkspace.id;
  const workspace = workspaces.find((item) => item.id === workspaceId);
  return (
    workspace?.name ||
    (workspaceId === defaultWorkspace.id ? defaultWorkspace.name : workspaceId || defaultWorkspace.name)
  );
}

function getWorkspaceIdFromPath() {
  if (typeof window === 'undefined') {
    return undefined;
  }
  return window.location.pathname.match(/^\/workspaces\/([^/]+)\/(?:cost|logs)(?:\/|$)/)?.[1];
}
