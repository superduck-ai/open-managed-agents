import { ArrowDown, ArrowUp } from 'lucide-react';
import { useState } from 'react';
import { Area, AreaChart, YAxis } from 'recharts';
import { useI18n } from '../../../shared/i18n';
import { ChartContainer } from '../../../shared/ui/chart';
import { ToggleGroup, ToggleGroupItem } from '../../../shared/ui/toggle-group';
import type { PanelUnit, PercentilesData, SeriesPoint, StatData } from '../api';
import { changeRatio, formatValue } from './format';
import { EmptyPanel, PanelFrame } from './PanelFrame';

function Change({ data }: { data: StatData }) {
  const { locale, msg } = useI18n();
  const ratio = changeRatio(data.value, data.previous);
  const Arrow = ratio != null && ratio < 0 ? ArrowDown : ArrowUp;
  return (
    <span className="flex items-center gap-1 text-xs text-muted-foreground">
      {msg('observability.previousPeriod', 'vs previous period')}
      {ratio == null ? (
        ' —'
      ) : (
        <span className="flex items-center tabular-nums text-foreground">
          <Arrow className="size-3" aria-hidden />
          {ratio.toLocaleString(locale, { style: 'percent', maximumFractionDigits: 1, signDisplay: 'exceptZero' })}
        </span>
      )}
    </span>
  );
}

const SPARKLINE_CONFIG = { v: { color: 'var(--chart-1)' } };

function Sparkline({ points }: { points: SeriesPoint[] }) {
  if (!points.some((point) => !!point.v)) return null;
  return (
    <ChartContainer config={SPARKLINE_CONFIG} className="pointer-events-none aspect-auto h-10 w-full" aria-hidden>
      <AreaChart data={points} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
        <YAxis hide domain={[0, 'auto']} />
        <Area
          dataKey="v"
          type="monotone"
          stroke="var(--color-v)"
          fill="var(--color-v)"
          fillOpacity={0.12}
          strokeWidth={1.5}
          dot={false}
          connectNulls
          isAnimationActive={false}
        />
      </AreaChart>
    </ChartContainer>
  );
}

export function StatPanel({
  id,
  unit,
  compare,
  data,
}: {
  id: string;
  unit?: PanelUnit;
  compare?: boolean;
  data: StatData;
}) {
  const { locale } = useI18n();
  if (data.value == null)
    return (
      <PanelFrame id={id}>
        <EmptyPanel />
      </PanelFrame>
    );
  return (
    <PanelFrame id={id}>
      <div className="flex items-baseline gap-2">
        <span className="text-2xl font-semibold tracking-tight tabular-nums">
          {formatValue(unit, data.value, locale)}
        </span>
        {data.secondary !== undefined ? (
          <span className="text-xs tabular-nums text-muted-foreground">
            p95 {formatValue(unit, data.secondary, locale)}
          </span>
        ) : null}
      </div>
      {compare ? <Change data={data} /> : null}
      {data.trend ? (
        <div className="mt-auto">
          <Sparkline points={data.trend} />
        </div>
      ) : null}
    </PanelFrame>
  );
}

const PERCENTILES = ['p50', 'p90', 'p95'] as const;

function PercentileToggle({
  value,
  onChange,
}: {
  value: (typeof PERCENTILES)[number];
  onChange: (value: (typeof PERCENTILES)[number]) => void;
}) {
  const { msg } = useI18n();
  return (
    <ToggleGroup
      aria-label={msg('observability.percentile', 'Percentile')}
      value={[value]}
      onValueChange={(next) => next.length && onChange(next[0] as (typeof PERCENTILES)[number])}
      size="sm"
    >
      {PERCENTILES.map((percentile) => (
        <ToggleGroupItem key={percentile} value={percentile} className="h-5 px-2 font-mono text-[11px]">
          {percentile}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  );
}

export function PercentilesPanel({ id, unit, data }: { id: string; unit?: PanelUnit; data: PercentilesData }) {
  const { locale, msg } = useI18n();
  const [percentile, setPercentile] = useState<(typeof PERCENTILES)[number]>('p50');
  const empty = PERCENTILES.every((key) => data[key] == null);
  return (
    <PanelFrame id={id}>
      {empty ? (
        <EmptyPanel />
      ) : (
        <>
          <span className="text-2xl font-semibold tracking-tight tabular-nums">
            {formatValue(unit, data[percentile], locale)}
          </span>
          <div className="mt-auto flex items-center justify-between gap-2">
            <span className="text-xs text-muted-foreground">{msg('observability.perSession', 'Per session')}</span>
            <PercentileToggle value={percentile} onChange={setPercentile} />
          </div>
        </>
      )}
    </PanelFrame>
  );
}
