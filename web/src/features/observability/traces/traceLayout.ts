import type { ObservabilitySpan } from '../types';
import { spanServiceName } from './traceTree';

export { spanAgentID } from './traceTree';

export const TRACE_ROW_HEIGHT = 30;
export const TRACE_BAR_HEIGHT = 8;
export const TRACE_TREE_GAP = 15;
export const TRACE_COLLAPSE_WIDTH = 14;
export const TRACE_DEFAULT_LEFT_WIDTH = 460;
export const TRACE_MIN_LEFT_WIDTH = 240;
export const TRACE_MAX_LEFT_WIDTH = 720;
export const TRACE_DURATION_LABEL_WIDTH = 60;

const CHART_COLORS = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)'];

export type DurationLabelPlacement = {
  top: string;
  left?: string;
  right?: number;
};

export function spanIdentity(span: ObservabilitySpan) {
  return span.attributes.infer_service_name || span.attributes.infer_service_system || spanServiceName(span);
}

export function resolveAgentName(agentID: string, names: ReadonlyMap<string, string>) {
  if (!agentID) {
    return '';
  }
  return names.get(agentID) || agentID;
}

export function spanColorKey(span: ObservabilitySpan) {
  return (
    span.attributes.infer_service_name || span.attributes.infer_service_system || span.kind || spanServiceName(span)
  );
}

export function serviceColorMap(keys: string[]) {
  const colors = new Map<string, string>();
  for (const key of keys) {
    if (!colors.has(key)) {
      colors.set(key, CHART_COLORS[colors.size % CHART_COLORS.length]);
    }
  }
  return colors;
}

export function spanTokenTotal(span: ObservabilitySpan) {
  return ['input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_creation_tokens'].reduce(
    (sum, key) => sum + (Number(span.attributes[key]) || 0),
    0,
  );
}

export function treeIndentLeft(depth: number) {
  return TRACE_TREE_GAP * depth;
}

export function connectorOffset(depth: number, visualDepth: number) {
  return treeIndentLeft(depth) - TRACE_TREE_GAP * (visualDepth - 1);
}

export function showTreeConnector(ancestorLast: boolean[], depth: number, visualDepth: number) {
  if (visualDepth === 1) {
    return true;
  }
  return !ancestorLast[depth - visualDepth + 1];
}

export function connectorHeight(isLast: boolean, visualDepth: number) {
  if (isLast && visualDepth === 1) {
    return TRACE_ROW_HEIGHT / 2;
  }
  return TRACE_ROW_HEIGHT;
}

export function durationLabelPlacement(
  offsetPct: number,
  widthPct: number,
  containerWidth: number,
  labelWidth = TRACE_DURATION_LABEL_WIDTH,
): DurationLabelPlacement {
  if (containerWidth <= 0) {
    return { top: '0.625rem', left: `${Math.min(99, offsetPct + Math.max(widthPct, 1))}%` };
  }
  const onePercent = Number((containerWidth / 100).toFixed(2));
  if ((offsetPct + widthPct) * onePercent + labelWidth > containerWidth) {
    return { top: '0.125rem', right: 0 };
  }
  if (offsetPct > 50) {
    return { top: '0.625rem', left: `${offsetPct * onePercent - labelWidth}px` };
  }
  const barEndPct = offsetPct + (Math.floor(widthPct) ? widthPct : 1);
  const barPx = barEndPct * onePercent - offsetPct * onePercent;
  const leftPx = barPx < 19 ? offsetPct * onePercent + 19 : barEndPct * onePercent;
  return { top: '0.625rem', left: `${leftPx}px` };
}
