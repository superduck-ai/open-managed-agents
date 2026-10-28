import type { DashboardPanel, ObservabilityDashboard } from '../api';
import { PanelContext } from './context';
import { BarsPanel, TablePanel } from './ListPanels';
import { SeriesPanel } from './SeriesPanel';
import { PercentilesPanel, StatPanel } from './StatPanels';

const SPAN_CLASSES: Record<number, string> = {
  3: 'md:col-span-6 xl:col-span-3',
  6: 'md:col-span-12 xl:col-span-6',
};

function PanelView({ panel, intervalMs }: { panel: DashboardPanel; intervalMs: number }) {
  switch (panel.render) {
    case 'stat':
      return <StatPanel id={panel.id} unit={panel.unit} compare={panel.compare} data={panel.data} />;
    case 'percentiles':
      return <PercentilesPanel id={panel.id} unit={panel.unit} data={panel.data} />;
    case 'series':
      return (
        <SeriesPanel
          id={panel.id}
          unit={panel.unit}
          chart={panel.chart}
          stacked={panel.stacked}
          data={panel.data}
          intervalMs={intervalMs}
        />
      );
    case 'bars':
      return <BarsPanel id={panel.id} unit={panel.unit} data={panel.data} />;
    default:
      return <TablePanel id={panel.id} columns={panel.columns ?? []} rows={panel.data.rows} />;
  }
}

export function DashboardGrid({ dashboard, viewPanel }: { dashboard: ObservabilityDashboard; viewPanel: string }) {
  const viewed = dashboard.panels.find((panel) => panel.id === viewPanel);
  const panels = viewed ? [viewed] : dashboard.panels;
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-12">
      {panels.map((panel) => (
        <div
          key={panel.id}
          className={`min-w-0 ${viewed ? 'md:col-span-12' : (SPAN_CLASSES[panel.span] ?? 'md:col-span-12')}`}
        >
          <PanelContext value={panel}>
            <PanelView panel={panel} intervalMs={dashboard.interval_ms} />
          </PanelContext>
        </div>
      ))}
    </div>
  );
}
