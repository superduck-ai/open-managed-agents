import { useI18n } from '@/shared/i18n';
import { Alert, AlertDescription } from '@/shared/ui/alert';
import { AnalyticsPageRoot, FilterBar, FilterControl } from './analytics-chrome';
import { UsageResults } from './UsageResults';
import { monthOptions } from './usage-api';
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
      {!scope.orgUuid ? (
        <Alert>
          <AlertDescription>
            {msg('analytics.real.selectOrg', 'Select an organization to view usage.')}
          </AlertDescription>
        </Alert>
      ) : null}
      {(query.isPending || query.isPlaceholderData) && scope.orgUuid ? (
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
      {query.data && !query.isError && !query.isPlaceholderData ? (
        <UsageResults report={query.data} cost={cost} />
      ) : null}
    </AnalyticsPageRoot>
  );
}
