import { useState } from 'react';
import { Line, LineChart } from 'recharts';
import { ChartContainer, type ChartConfig } from '../../../shared/ui/chart';
import { ToggleGroup, ToggleGroupItem } from '../../../shared/ui/toggle-group';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { chartColorAt } from '../chartColors';
import { formatStatValue } from '../format';
import { fillTimeseriesRows, mergeTimeseriesRows, multistatItemNames, selectedMultistatName } from '../model';
import type { MultistatPanelData, ObservabilityPanelOptions, TimeseriesSeries } from '../types';

export function MultiStatPanel({
  unit,
  options,
  data,
  start,
  end,
}: {
  unit: string;
  options?: ObservabilityPanelOptions;
  data: MultistatPanelData;
  start?: string;
  end?: string;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const items = data.items ?? [];
  const names = multistatItemNames(items);
  const [selected, setSelected] = useState(names[0] ?? '');
  const name = selectedMultistatName(items, selected);
  const current = items.find((item) => item.name === name);
  const subtitleKey = options?.subtitle_key;
  const subtitle = subtitleKey ? msg(subtitleKey, '') : '';

  return (
    <div className="flex min-h-16 items-end justify-between gap-3">
      <div className="flex min-w-0 flex-col gap-1">
        <p className="text-2xl font-semibold tracking-tight text-foreground tabular-nums">
          {formatStatValue(current?.value, unit, formatters)}
        </p>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          {subtitle ? <p className="text-xs text-muted-foreground">{subtitle}</p> : null}
          {names.length > 1 ? (
            <ToggleGroup
              multiple={false}
              value={name ? [name] : []}
              onValueChange={(next) => {
                if (next[0]) {
                  setSelected(next[0]);
                }
              }}
              size="sm"
              className="h-6 p-px"
              aria-label={msg('observability.multistat.selector', 'Value')}
            >
              {names.map((key) => (
                <ToggleGroupItem key={key} value={key} className="h-5 min-h-5 min-w-0 px-1.5 text-[11px]">
                  {msg(`observability.multistat.${key}`, key)}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          ) : null}
        </div>
      </div>
      <MultistatSparkline
        name={name}
        series={data.series ?? []}
        start={start}
        end={end}
        label={msg('observability.multistat.sparkline', 'Trend')}
      />
    </div>
  );
}

function MultistatSparkline({
  name,
  series,
  start,
  end,
  label,
}: {
  name: string;
  series: TimeseriesSeries[];
  start?: string;
  end?: string;
  label: string;
}) {
  const selected = series.find((item) => item.name === name);
  if (!selected?.points.length) {
    return null;
  }
  const startMs = start ? Date.parse(start) : Number.NaN;
  const endMs = end ? Date.parse(end) : Number.NaN;
  const merged = mergeTimeseriesRows([selected]);
  const rows =
    Number.isFinite(startMs) && Number.isFinite(endMs)
      ? fillTimeseriesRows(merged, [name], startMs, endMs, 'gap')
      : merged;
  const config: ChartConfig = { [name]: { label, color: chartColorAt(0) } };
  return (
    <ChartContainer config={config} className="h-8 w-20 min-w-16 flex-none aspect-auto">
      <LineChart data={rows} margin={{ top: 2, right: 0, left: 0, bottom: 2 }}>
        <Line
          type="monotone"
          dataKey={name}
          stroke={chartColorAt(0)}
          strokeWidth={1.5}
          dot={false}
          isAnimationActive={false}
          connectNulls={false}
        />
      </LineChart>
    </ChartContainer>
  );
}
