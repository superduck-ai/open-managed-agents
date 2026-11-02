import type { RawTimeRange } from './range';

export interface QuickRange extends RawTimeRange {
  key: string;
  text: string;
}

export const QUICK_RANGES: QuickRange[] = [
  { key: '5m', from: 'now-5m', to: 'now', text: 'Last 5 minutes' },
  { key: '15m', from: 'now-15m', to: 'now', text: 'Last 15 minutes' },
  { key: '30m', from: 'now-30m', to: 'now', text: 'Last 30 minutes' },
  { key: '1h', from: 'now-1h', to: 'now', text: 'Last 1 hour' },
  { key: '3h', from: 'now-3h', to: 'now', text: 'Last 3 hours' },
  { key: '6h', from: 'now-6h', to: 'now', text: 'Last 6 hours' },
  { key: '12h', from: 'now-12h', to: 'now', text: 'Last 12 hours' },
  { key: '24h', from: 'now-24h', to: 'now', text: 'Last 24 hours' },
  { key: '2d', from: 'now-2d', to: 'now', text: 'Last 2 days' },
  { key: '7d', from: 'now-7d', to: 'now', text: 'Last 7 days' },
  { key: '30d', from: 'now-30d', to: 'now', text: 'Last 30 days' },
  { key: '90d', from: 'now-90d', to: 'now', text: 'Last 90 days' },
  { key: '6M', from: 'now-6M', to: 'now', text: 'Last 6 months' },
  { key: '1y', from: 'now-1y', to: 'now', text: 'Last 1 year' },
  { key: 'today', from: 'now/d', to: 'now/d', text: 'Today' },
  { key: 'todaySoFar', from: 'now/d', to: 'now', text: 'Today so far' },
  { key: 'yesterday', from: 'now-1d/d', to: 'now-1d/d', text: 'Yesterday' },
  { key: 'thisWeek', from: 'now/w', to: 'now/w', text: 'This week' },
  { key: 'thisWeekSoFar', from: 'now/w', to: 'now', text: 'This week so far' },
  { key: 'lastWeek', from: 'now-1w/w', to: 'now-1w/w', text: 'Previous week' },
  { key: 'thisMonth', from: 'now/M', to: 'now/M', text: 'This month' },
  { key: 'thisMonthSoFar', from: 'now/M', to: 'now', text: 'This month so far' },
  { key: 'lastMonth', from: 'now-1M/M', to: 'now-1M/M', text: 'Previous month' },
];

export function findQuickRange(raw: RawTimeRange) {
  return QUICK_RANGES.find((range) => range.from === raw.from && range.to === raw.to);
}
