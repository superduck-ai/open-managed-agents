import { createContext, useContext } from 'react';
import type { SessionOutputFile } from './sessionOutputModel';

export const SessionFileContext = createContext<{
  files: SessionOutputFile[];
  workspaceId: string;
  running: boolean;
} | null>(null);
export function useSessionFiles() {
  return useContext(SessionFileContext);
}
