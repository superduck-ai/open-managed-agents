import { describe, expect, test } from 'bun:test';
import {
  CATEGORY_AXIS_MAX_WIDTH,
  CATEGORY_AXIS_MIN_WIDTH,
  categoryAxisWidth,
  categoryValueAxisMax,
  formatChartHoverTimestamp,
  formatHeaderTimestamp,
  formatTimeRangeAbsolute,
  formatTimeseriesYTick,
  formatSeriesParts,
  panelCategoryLabel,
  panelSeriesLabel,
  splitCompoundSeries,
  shortenResourceId,
  truncateCategoryTick,
} from './format';

describe('observability timestamp format', () => {
  test('returns undefined for chart hover labels that are not timestamps', () => {
    expect(formatChartHoverTimestamp(undefined)).toBeUndefined();
    expect(formatChartHoverTimestamp('')).toBeUndefined();
    expect(formatChartHoverTimestamp(Number.NaN)).toBeUndefined();
    expect(formatChartHoverTimestamp({})).toBeUndefined();
  });

  test('formats chart hover timestamps as YYYY-MM-DD HH:mm:ss in local time', () => {
    const local = new Date(2026, 7, 13, 14, 56, 47);
    expect(formatHeaderTimestamp(local.toISOString())).toBe('2026-08-13 14:56:47');
    expect(formatChartHoverTimestamp(local.getTime())).toBe('2026-08-13 14:56:47');
    expect(formatChartHoverTimestamp(local.toISOString())).toBe('2026-08-13 14:56:47');
  });

  test('joins an absolute range with a tilde', () => {
    const start = new Date(2026, 7, 13, 15, 56, 59).toISOString();
    const end = new Date(2026, 7, 13, 16, 11, 59).toISOString();
    expect(formatTimeRangeAbsolute(start, end)).toBe('2026-08-13 15:56:59 ~ 2026-08-13 16:11:59');
  });
});

describe('timeseries y-axis ticks', () => {
  test('hides the origin tick so it does not collide with the first time label', () => {
    expect(formatTimeseriesYTick(0, '0')).toBe('');
    expect(formatTimeseriesYTick(0, '0ms')).toBe('');
    expect(formatTimeseriesYTick(0, '0%')).toBe('');
    expect(formatTimeseriesYTick(1e-9, '0')).toBe('');
  });

  test('keeps non-origin ticks', () => {
    expect(formatTimeseriesYTick(25_0000, '25万')).toBe('25万');
    expect(formatTimeseriesYTick(4, '4')).toBe('4');
    expect(formatTimeseriesYTick(-2, '-2')).toBe('-2');
  });
});

describe('panel named labels', () => {
  const msg = (id: string, fallback: string) => {
    if (id === 'observability.series.cacheRead') {
      return '缓存读取';
    }
    if (id === 'observability.category.lt_1s') {
      return '<1s';
    }
    return fallback;
  };

  test('translates known series names and leaves model names unchanged', () => {
    expect(panelSeriesLabel('cacheRead', msg)).toBe('缓存读取');
    expect(panelSeriesLabel('claude-sonnet-4', msg)).toBe('claude-sonnet-4');
    expect(panelSeriesLabel('', msg)).toBe('—');
  });

  test('splits model · metric series so the suffix can stay visible when the name truncates', () => {
    expect(splitCompoundSeries('claude-haiku-4-5-20251001 · p95')).toEqual({
      base: 'claude-haiku-4-5-20251001',
      suffix: 'p95',
    });
    expect(splitCompoundSeries('claude-sonnet-4-6')).toBeNull();
    const parts = formatSeriesParts('claude-haiku-4-5-20251001 · avg', (id, fallback) =>
      id === 'observability.series.avg' ? '平均' : fallback,
    );
    expect(parts.base).toBe('claude-haiku-4-5-20251001');
    expect(parts.suffix).toBe('平均');
    expect(parts.full).toBe('claude-haiku-4-5-20251001 · 平均');
    expect(
      panelSeriesLabel('claude-haiku-4-5-20251001 · avg', (id, fallback) =>
        id === 'observability.series.avg' ? '平均' : fallback,
      ),
    ).toBe('claude-haiku-4-5-20251001 · 平均');
  });

  test('translates known category names', () => {
    expect(panelCategoryLabel('lt_1s', msg)).toBe('<1s');
    expect(panelCategoryLabel('unknown_bucket', msg)).toBe('unknown_bucket');
  });
});

describe('categorical bar axis layout', () => {
  test('sizes the y-axis so model names are not clipped', () => {
    expect(categoryAxisWidth(['a'])).toBe(CATEGORY_AXIS_MIN_WIDTH);
    expect(categoryAxisWidth(['claude-sonnet-4-6'])).toBeGreaterThan(96);
    expect(categoryAxisWidth(['x'.repeat(80)])).toBe(CATEGORY_AXIS_MAX_WIDTH);
  });

  test('ellipsis ticks that exceed the capped axis width', () => {
    const label = 'claude-sonnet-4-6';
    expect(truncateCategoryTick(label, categoryAxisWidth([label]))).toBe(label);
    const truncated = truncateCategoryTick('anthropic-claude-sonnet-4-6-long', CATEGORY_AXIS_MAX_WIDTH);
    expect(truncated.endsWith('…')).toBe(true);
    expect(truncated.startsWith('anthropic')).toBe(true);
  });

  test('pads the value axis so a full-scale bar is not flush with the edge', () => {
    expect(categoryValueAxisMax([])).toBe(1);
    expect(categoryValueAxisMax([0])).toBe(1);
    expect(categoryValueAxisMax([1_000_000])).toBe(1_080_000);
    expect(categoryValueAxisMax([3], true)).toBe(4);
    expect(categoryValueAxisMax([1_000_000], true)).toBe(1_080_000);
  });
});

describe('resource id shortening', () => {
  test('keeps short ids intact and preserves the unique suffix of long ids', () => {
    expect(shortenResourceId('agent_short')).toBe('agent_short');
    expect(shortenResourceId('agent_MrxBvq8uqLdwIheiTOOeaJPU')).toBe('agent_MrxBvq8uq…OeaJPU');
  });
});
