import { Bar, BarChart, CartesianGrid, Tooltip, XAxis, YAxis } from 'recharts';
import { useI18n } from '@/shared/i18n';
import { Alert, AlertDescription } from '@/shared/ui/alert';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { ChartContainer } from '@/shared/ui/chart';
import { MetricPanel } from './analytics-chrome';
import { money, type UsageReport } from './usage-api';

export function UsageResults({ report, cost }: { report: UsageReport; cost: boolean }) {
  const { msg, locale } = useI18n();
  const metrics = report.summary;
  const unpriced = metrics.unpriced_requests + metrics.incomplete_requests > 0;
  const cards = cost
    ? [
        {
          title: msg('analytics.cost.totalCost', 'Total cost'),
          value: money(metrics.subtotal_cents, locale),
        },
        {
          title: msg('analytics.cost.totalTokenCost', 'Total token cost'),
          value: money(metrics.model_cost_cents, locale),
        },
        {
          title: msg('analytics.cost.totalWebSearchCost', 'Total web search cost'),
          value: money(metrics.search_cost_cents, locale),
        },
        { title: msg('analytics.cost.totalCodeExecutionCost', 'Total code execution cost'), value: money('0', locale) },
        {
          title: msg('analytics.cost.totalSessionRuntimeCost', 'Total session runtime cost'),
          value: money('0', locale),
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
    value: cost
      ? Number(bucket.metrics.model_cost_cents) / 100
      : bucket.metrics.input_tokens +
        bucket.metrics.cache_read_input_tokens +
        bucket.metrics.cache_creation_input_tokens +
        bucket.metrics.output_tokens,
  }));
  return (
    <>
      {cost && unpriced ? (
        <Alert>
          <AlertDescription>
            {msg(
              'analytics.real.incomplete',
              'Some requests have missing prices or incomplete usage. Shown costs are the known subtotal, not the full cost.',
            )}
          </AlertDescription>
        </Alert>
      ) : null}
      <div className={cost ? 'grid gap-3 md:grid-cols-3 xl:grid-cols-5' : 'grid gap-3 md:grid-cols-3'}>
        {cards.map((card) => (
          <MetricPanel key={card.title} {...card} />
        ))}
      </div>
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent>
          {metrics.requests === 0 ? (
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
                  tickFormatter={(value) =>
                    new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC' }).format(
                      new Date(String(value)),
                    )
                  }
                />
                <YAxis
                  width={80}
                  tickMargin={8}
                  tickFormatter={(value: number) =>
                    new Intl.NumberFormat(locale, {
                      notation: 'compact',
                      maximumFractionDigits: 2,
                    }).format(value)
                  }
                />
                <Tooltip
                  content={({ active, payload, label }) => {
                    if (!active || !payload?.length) return null;
                    const bucket = payload[0].payload;
                    return (
                      <div className="rounded-lg border-[0.5px] border-border bg-popover p-3 text-sm shadow-sm">
                        <p>
                          {new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(
                            new Date(String(label)),
                          )}
                        </p>
                        {cost ? (
                          <p>{money(String(Math.round(bucket.value * 100)), locale)}</p>
                        ) : (
                          <>
                            <p>
                              {msg('analytics.real.uncached', 'Uncached input tokens')}:{' '}
                              {bucket.input.toLocaleString(locale)}
                            </p>
                            <p>
                              {msg('analytics.real.cacheRead', 'Cache read tokens')}:{' '}
                              {bucket.cacheRead.toLocaleString(locale)}
                            </p>
                            <p>
                              {msg('analytics.real.cacheWrite', 'Cache write tokens')}:{' '}
                              {bucket.cacheWrite.toLocaleString(locale)}
                            </p>
                            <p>
                              {msg('analytics.usage.totalTokensOut', 'Total tokens out')}:{' '}
                              {bucket.output.toLocaleString(locale)}
                            </p>
                          </>
                        )}
                      </div>
                    );
                  }}
                />
                <Bar dataKey="value" fill="var(--color-value)" radius={3} />
              </BarChart>
            </ChartContainer>
          )}
        </CardContent>
      </Card>
    </>
  );
}
