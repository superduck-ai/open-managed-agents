import { createContext, useContext } from 'react';
import type { AuthAccount } from '../auth/api';
import { defaultWorkspace, type CreateWorkspaceInput, type Workspace } from './api';

export type WorkspaceContextValue = {
  orgUuid?: string;
  workspaces: Workspace[];
  activeWorkspace: Workspace;
  activeWorkspaceId: string;
  isLoading: boolean;
  error: unknown;
  selectWorkspace: (workspaceId: string) => void;
  createWorkspace: (input: CreateWorkspaceInput) => Promise<Workspace>;
  refreshWorkspaces: () => Promise<void>;
};

const fallbackWorkspaceContext: WorkspaceContextValue = {
  workspaces: [defaultWorkspace],
  activeWorkspace: defaultWorkspace,
  activeWorkspaceId: defaultWorkspace.id,
  isLoading: false,
  error: null,
  selectWorkspace: () => undefined,
  createWorkspace: async (input) => ({
    ...defaultWorkspace,
    id: `wrkspc_${input.name.toLowerCase().replace(/[^a-z0-9]+/g, '_')}`,
    name: input.name,
    display_color: input.display_color,
    color: input.display_color,
    data_residency: input.data_residency,
  }),
  refreshWorkspaces: async () => undefined,
};

export const WorkspaceContext = createContext<WorkspaceContextValue | null>(null);

export function useWorkspace() {
  return useContext(WorkspaceContext) ?? fallbackWorkspaceContext;
}

export function getPrimaryOrganizationUuid(account?: AuthAccount | null) {
  return account?.memberships?.find((membership) => membership.organization?.uuid)?.organization?.uuid;
}
