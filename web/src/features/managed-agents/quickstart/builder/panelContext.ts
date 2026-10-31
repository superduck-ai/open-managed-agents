import { createContext, useContext } from 'react';

export type BuilderPanel = { kind: 'overview' | 'agent' } | { kind: 'environment'; id: string; name: string };
export const BuilderPanelContext = createContext<(panel: BuilderPanel) => void>(() => {});
export function useBuilderPanel() {
  return useContext(BuilderPanelContext);
}
