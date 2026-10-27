import { useI18n } from '@/shared/i18n';
import { Alert, AlertDescription } from '@/shared/ui/alert';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { AnalyticsPageRoot, FilterBar, FilterControl } from './analytics-chrome';
import { RuntimeCostChart } from './RuntimeCostChart';
import {
  apiErrorStatus,
  isRuntimeCostReport,
  sessionRuntimeCostLabel,
  useRuntimeCost,
  type RuntimeCostReport,
} from './runtimeCost';
import { UsageResults } from './UsageResults';
import { money, monthOptions, reportRange, type UsageReport } from './usage-api';
import { useUsageReport } from './use-usage-report';

export function UsagePage() {
  return <MeteredAnalyticsPage cost={false} />;
}
export function CostPage() {
  return <MeteredAnalyticsPage cost />;
}

function MeteredAnalyticsPage({ cost }: { cost: boolean }) {
  const { msg, locale } = useI18n();
  const { scope, filters, change, query, workspaces } = useUsageReport(cost);
  const options = query.data?.options;
  const all = { value: '', label: msg('common.all', 'All') };
  const workspaceOptions = [all, ...workspaces];
  return (
    <AnalyticsPageRoot title={cost ? msg('analytics.cost.title', 'Cost') : msg('analytics.usage.title', 'Usage')}>
      <p className="text-sm text-muted-foreground">
        {cost
          ? msg('analytics.real.costDescription', 'Your organization’s usage costs over time. All dates are in UTC.')
          : msg('analytics.real.usageDescription', 'Your organization’s token usage over time. All dates are in UTC.')}
      </p>
      <FilterBar>
        <FilterControl
          label={msg('analytics.filter.workspace', 'Workspace')}
          value={filters.workspace}
          options={workspaceOptions}
          onValueChange={(value) => change('workspace', value)}
        />
        <FilterControl
          label={msg('analytics.filter.range', 'Range')}
          value={filters.range}
          options={[
            { value: 'last_30_days', label: msg('analytics.filter.last30Days', 'Last 30 days') },
            { value: 'last_7_days', label: msg('analytics.filter.last7Days', 'Last 7 days') },
            { value: 'month_to_date', label: msg('analytics.filter.monthToDate', 'Month to date') },
            ...monthOptions(options?.first_seen ?? null, locale),
          ]}
          onValueChange={(value) => change('range', value)}
        />
        <FilterControl
          label={msg('analytics.filter.model', 'Model')}
          value={filters.model}
          options={[all, ...(options?.models ?? []).map((value) => ({ value, label: value }))]}
          onValueChange={(value) => change('model', value)}
        />
        <FilterControl
          label={msg('analytics.real.apiKey', 'API key')}
          value={filters.apiKey}
          options={[all, ...(options?.api_keys ?? []).map((value) => ({ value, label: value }))]}
          onValueChange={(value) => change('apiKey', value)}
        />
      </FilterBar>
      <MeteredAnalyticsBody
        cost={cost}
        locale={locale}
        orgUuid={scope.orgUuid}
        workspaceId={filters.workspace}
        model={filters.model}
        range={filters.range}
        query={query}
      />
    </AnalyticsPageRoot>
  );
}

function MeteredAnalyticsBody({
  cost,
  locale,
  orgUuid,
  workspaceId,
  model,
  range,
  query,
}: {
  cost: boolean;
  locale: string;
  orgUuid?: string;
  workspaceId: string;
  model: string;
  range: string;
  query: {
    data?: UsageReport;
    isPending: boolean;
    isPlaceholderData: boolean;
    isError: boolean;
  };
}) {
  const { msg } = useI18n();
  const bounds = reportRange(range);
  const runtime = useRuntimeCost({
    orgUuid: orgUuid ?? '',
    workspaceId: workspaceId || 'all-workspaces',
    model: model || 'all',
    groupBy: 'model',
    range,
    startingOn: bounds.start.slice(0, 10),
    endingBefore: bounds.end.slice(0, 10),
    enabled: cost,
  });
  const runtimeReport = isRuntimeCostReport(runtime.data) ? runtime.data : undefined;
  return (
    <>
      {!orgUuid ? (
        <Alert>
          <AlertDescription>
            {msg('analytics.real.selectOrg', 'Select an organization to view usage.')}
          </AlertDescription>
        </Alert>
      ) : null}
      {(query.isPending || query.isPlaceholderData) && orgUuid ? (
        <p role="status">{msg('common.loading', 'Loading...')}</p>
      ) : null}
      {query.isError ? (
        <Alert variant="destructive">
          <AlertDescription>
            {cost
              ? msg('analytics.real.costFailed', 'Could not load costs. Check your access and retry.')
              : msg('analytics.real.failed', 'Could not load usage. Check your access and retry.')}
          </AlertDescription>
        </Alert>
      ) : null}
      {cost ? <RuntimeCostNotice report={runtimeReport} error={runtime.error} /> : null}
      {query.data && !query.isError && !query.isPlaceholderData ? (
        <UsageResults
          report={query.data}
          cost={cost}
          runtimeValue={cost ? runtimeCardValue(runtimeReport, locale, msg) : undefined}
        />
      ) : null}
    </>
  );
}

function runtimeCardValue(
  report: RuntimeCostReport | undefined,
  locale: string,
  msg: (id: string, fallback: string) => string,
) {
  return sessionRuntimeCostLabel(report, money('0', locale), msg('analytics.cost.notConfigured', 'Not configured'));
}

function RuntimeCostNotice({ report, error }: { report: RuntimeCostReport | undefined; error: unknown }) {
  const { msg } = useI18n();
  const days = report?.daily.some((day) => day.groups.length > 0) ? report.daily : undefined;
  return (
    <>
      {report?.configured === false ? (
        <Alert>
          <AlertDescription>
            {msg('analytics.cost.runtimePriceMissing', 'Runtime price is not configured')}
          </AlertDescription>
        </Alert>
      ) : null}
      {apiErrorStatus(error) === 403 ? (
        <Alert>
          <AlertDescription>
            {msg('analytics.cost.accessDenied', 'Only organization administrators can view cost across workspaces.')}
          </AlertDescription>
        </Alert>
      ) : null}
      {days ? (
        <Card>
          <CardHeader>
            <CardTitle>{msg('analytics.cost.dailyRuntimeCost', 'Daily runtime cost')}</CardTitle>
          </CardHeader>
          <CardContent>
            <RuntimeCostChart days={days} />
          </CardContent>
        </Card>
      ) : null}
    </>
  );
}
