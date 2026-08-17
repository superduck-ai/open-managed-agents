// 图表配色的单一来源：时间序列、饼图、瀑布图共用同一色板，保证跨面板视觉一致。
export const CHART_COLORS = [
  'var(--chart-1)',
  'var(--chart-2)',
  'var(--chart-3)',
  'var(--chart-4)',
  'var(--chart-5)',
] as const;

export function chartColorAt(index: number): string {
  return CHART_COLORS[index % CHART_COLORS.length];
}
