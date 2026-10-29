import { createContext, useContext } from 'react';
import type { DashboardPanel } from '../api';
import type { TimeZoneMode } from '../time/range';

export interface DashboardInteractions {
  syncId: string;
  timeZone: TimeZoneMode;
  viewPanel: string;
  fetching: boolean;
  onZoom: (start: number, end: number) => void;
  onViewPanel: (id: string) => void;
  onInspect: (panel: DashboardPanel) => void;
  onHoverPanel: (id: string) => void;
}

export const DashboardContext = createContext<DashboardInteractions | null>(null);

export const PanelContext = createContext<DashboardPanel | null>(null);

export function useDashboardInteractions() {
  return useContext(DashboardContext);
}
