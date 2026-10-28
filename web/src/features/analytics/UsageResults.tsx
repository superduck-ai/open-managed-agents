import type { ReactNode } from 'react';
import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from 'recharts';
import { useI18n } from '@/shared/i18n';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from '@/shared/ui/chart';
import { MetricPanel } from './analytics-chrome';
import { costTotalLabel, formatCost, modelCostDollars, money, toolCostDollars, type UsageReport } from './usage-api';

export function UsageResults({
  report,
  cost,
  runtimeValue,
  runtimeDollars = '0',
  afterSummary,
}: {
  report: UsageReport;
  cost: boolean;
  runtimeValue?: string;
  runtimeDollars?: string;
  afterSummary?: ReactNode;
}) {
  const { msg, locale } = useI18n();
  const metrics = report.summary;
  const cards = cost
    ? [
        {
          title: msg('analytics.cost.totalCost', 'Total cost'),
          value: costTotalLabel(modelCostDollars(metrics), toolCostDollars(metrics), runtimeDollars, locale),
        },
        {
          title: msg('analytics.cost.totalTokenCost', 'Total token cost'),
          value: formatCost(modelCostDollars(metrics), locale),
        },
        {
          title: msg('analytics.cost.totalToolCallCost', 'Total tool call cost'),
          value: formatCost(toolCostDollars(metrics), locale),
        },
        {
          title: msg('analytics.cost.totalSessionRuntimeCost', 'Total session runtime cost'),
          value: runtimeValue ?? money('0', locale),
        },
      ]
    : [
        {
          title: msg('analytics.usage.totalTokensIn', 'Total tokens in'),
          value: (
            metrics.input_tokens +
            metrics.cache_read_input_tokens +
            metrics.cache_creation_input_tokens
          ).toLocaleString(locale),
        },
        {
          title: msg('analytics.usage.totalTokensOut', 'Total tokens out'),
          value: metrics.output_tokens.toLocaleString(locale),
        },
        {
          title: msg('analytics.usage.totalWebSearches', 'Total web searches'),
          value: metrics.web_search_requests.toLocaleString(locale),
        },
      ];
  const title = cost
    ? msg('analytics.cost.dailyTokenCost', 'Daily token cost')
    : msg('analytics.usage.tokenUsage', 'Token usage');
  const data = report.daily.map((bucket) => ({
    date: bucket.key,
    input: bucket.metrics.input_tokens,
    cacheRead: bucket.metrics.cache_read_input_tokens,
    cacheWrite: bucket.metrics.cache_creation_input_tokens,
    output: bucket.metrics.output_tokens,
    value:
      bucket.metrics.input_tokens +
      bucket.metrics.cache_read_input_tokens +
      bucket.metrics.cache_creation_input_tokens +
      bucket.metrics.output_tokens,
  }));
  return (
    <>
      <div className={cost ? 'grid gap-3 md:grid-cols-2 xl:grid-cols-4' : 'grid gap-3 md:grid-cols-3'}>
        {cards.map((card) => (
          <MetricPanel key={card.title} {...card} />
        ))}
      </div>
      {afterSummary}
      {cost ? (
        <NamedCostChart
          title={title}
          report={report}
          empty={metrics.requests === 0}
          locale={locale}
          names={seriesNames(report, (metrics) => metrics.model_costs, [])}
          costs={(metrics) => metrics.model_costs}
        />
      ) : (
        <DailySeriesChart title={title} data={data} empty={metrics.requests === 0} locale={locale} />
      )}
      {cost ? (
        <NamedCostChart
          title={msg('analytics.cost.dailyToolCallCost', 'Daily tool call cost')}
          report={report}
          empty={metrics.requests === 0 && metrics.web_search_requests === 0}
          locale={locale}
          names={seriesNames(report, (metrics) => metrics.tool_costs, ['web_search'])}
          costs={(metrics) => metrics.tool_costs}
        />
      ) : null}
    </>
  );
}

const toolPalette = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)'];

function NamedCostChart({
  title,
  report,
  empty,
  locale,
  names,
  costs,
}: {
  title: string;
  report: UsageReport;
  empty: boolean;
  locale: string;
  names: string[];
  costs: (metrics: UsageReport['summary']) => { name: string; cost: string }[] | undefined;
}) {
  const { msg } = useI18n();
  const config: ChartConfig = {};
  const rows = report.daily.map((bucket) => {
    const row: Record<string, string | number> = { date: bucket.key };
    const listed = costs(bucket.metrics);
    names.forEach((name, index) => {
      row[`series${index}`] = Number(listed?.find((item) => item.name === name)?.cost ?? '0');
      config[`series${index}`] = { label: name, color: toolPalette[index % toolPalette.length] };
    });
    return row;
  });
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {empty ? (
          <p className="py-16 text-center text-muted-foreground">{msg('common.noData', 'No data')}</p>
        ) : (
          <ChartContainer config={config} className="h-72 w-full min-w-0 aspect-auto">
            <BarChart data={rows} accessibilityLayer margin={{ left: 8, right: 16 }}>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="date"
                tickLine={false}
                axisLine={false}
                tickFormatter={(value) => String(value).slice(5, 10)}
              />
              <YAxis
                width={96}
                tickMargin={8}
                tickLine={false}
                axisLine={false}
                domain={rows.every((row) => names.every((_, index) => !row[`series${index}`])) ? [0, 1] : undefined}
                tickFormatter={(value: number) => formatCost(String(value), locale)}
              />
              <ChartTooltip content={<ChartTooltipContent />} />
              {names.map((name, index) => (
                <Bar
                  key={name}
                  dataKey={`series${index}`}
                  stackId="costs"
                  fill={`var(--color-series${index})`}
                  radius={3}
                />
              ))}
            </BarChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  );
}

function seriesNames(
  report: UsageReport,
  costs: (metrics: UsageReport['summary']) => { name: string; cost: string }[] | undefined,
  fallback: string[],
) {
  const names = new Set<string>();
  for (const metrics of [report.summary, ...report.daily.map((day) => day.metrics)]) {
    costs(metrics)?.forEach((item) => names.add(item.name));
  }
  return names.size === 0 ? fallback : [...names];
}

type SeriesPoint = {
  date: string;
  value: number;
  input?: number;
  cacheRead?: number;
  cacheWrite?: number;
  output?: number;
};

function DailySeriesChart({
  title,
  data,
  empty,
  locale,
}: {
  title: string;
  data: SeriesPoint[];
  empty: boolean;
  locale: string;
}) {
  const { msg } = useI18n();
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {empty ? (
          <p className="py-16 text-center text-muted-foreground">{msg('common.noData', 'No data')}</p>
        ) : (
          <ChartContainer
            config={{ value: { label: title, color: 'var(--chart-1)' } }}
            className="h-72 w-full min-w-0 aspect-auto"
          >
            <BarChart data={data} accessibilityLayer margin={{ left: 8, right: 16 }}>
              <CartesianGrid vertical={false} />
              <XAxis
                dataKey="date"
                tickLine={false}
                axisLine={false}
                tickFormatter={(value) => String(value).slice(5, 10)}
              />
              <YAxis
                width={80}
                tickMargin={8}
                tickLine={false}
                axisLine={false}
                domain={data.every((point) => !point.value) ? [0, 1] : undefined}
                tickFormatter={(value: number) =>
                  new Intl.NumberFormat(locale, {
                    notation: 'compact',
                    maximumFractionDigits: 2,
                  }).format(value)
                }
              />
              <Tooltip content={(props) => <SeriesTooltip {...props} locale={locale} />} />
              <Bar dataKey="value" fill="var(--color-value)" radius={3} />
            </BarChart>
          </ChartContainer>
        )}
      </CardContent>
    </Card>
  );
}

function SeriesTooltip({
  active,
  payload,
  label,
  locale,
}: {
  active?: boolean;
  payload?: Array<{ payload: SeriesPoint }>;
  label?: string | number;
  locale: string;
}) {
  const { msg } = useI18n();
  const bucket = payload?.[0]?.payload;
  if (!active || !bucket) return null;
  return (
    <div className="rounded-lg border-[0.5px] border-border bg-popover p-3 text-sm shadow-sm">
      <p>{new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(String(label)))}</p>
      <p>
        {msg('analytics.real.uncached', 'Uncached input tokens')}: {bucket.input?.toLocaleString(locale)}
      </p>
      <p>
        {msg('analytics.real.cacheRead', 'Cache read tokens')}: {bucket.cacheRead?.toLocaleString(locale)}
      </p>
      <p>
        {msg('analytics.real.cacheWrite', 'Cache write tokens')}: {bucket.cacheWrite?.toLocaleString(locale)}
      </p>
      <p>
        {msg('analytics.usage.totalTokensOut', 'Total tokens out')}: {bucket.output?.toLocaleString(locale)}
      </p>
    </div>
  );
}
