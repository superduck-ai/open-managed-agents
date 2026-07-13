import { useCallback, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { setConsoleRequestContext } from "../api/client";
import { useAuth } from "../auth/context";
import {
  createConsoleWorkspace,
  defaultWorkspace,
  listConsoleWorkspaces,
  type CreateWorkspaceInput,
  type Workspace,
} from "./api";
import { getPrimaryOrganizationUuid, WorkspaceContext, type WorkspaceContextValue } from "./context";

const activeWorkspaceStorageKey = "oma.activeWorkspaceId";

export function WorkspaceProvider({ children }: { children: ReactNode }) {
  const { account, status } = useAuth();
  const queryClient = useQueryClient();
  const orgUuid = getPrimaryOrganizationUuid(account);
  const [preferredWorkspaceId, setPreferredWorkspaceId] = useState(readStoredWorkspaceId);

  const workspacesQuery = useQuery({
    queryKey: ["console", "workspaces", orgUuid],
    queryFn: () => listConsoleWorkspaces(orgUuid ?? ""),
    enabled: status === "authenticated" && Boolean(orgUuid),
    retry: false,
  });

  const workspaces = useMemo(() => normalizeWorkspaces(workspacesQuery.data), [workspacesQuery.data]);
  const activeWorkspaceId = useMemo(
    () =>
      workspaces.some((workspace) => workspace.id === preferredWorkspaceId)
        ? preferredWorkspaceId
        : defaultWorkspace.id,
    [preferredWorkspaceId, workspaces],
  );
  const activeWorkspace = useMemo(
    () => workspaces.find((workspace) => workspace.id === activeWorkspaceId) ?? defaultWorkspace,
    [activeWorkspaceId, workspaces],
  );

  useEffect(() => {
    writeStoredWorkspaceId(activeWorkspaceId);
  }, [activeWorkspaceId]);

  useLayoutEffect(() => {
    setConsoleRequestContext({
      organizationUuid: status === "authenticated" ? orgUuid : undefined,
      workspaceId: status === "authenticated" ? activeWorkspaceId : undefined,
    });
    return () => setConsoleRequestContext({});
  }, [activeWorkspaceId, orgUuid, status]);

  const selectWorkspace = useCallback((workspaceId: string) => {
    const nextWorkspaceId = workspaceId || defaultWorkspace.id;
    writeStoredWorkspaceId(nextWorkspaceId);
    setPreferredWorkspaceId(nextWorkspaceId);
  }, []);

  const createWorkspace = useCallback(
    async (input: CreateWorkspaceInput) => {
      if (!orgUuid) {
        throw new Error("No organization is available for workspace creation.");
      }
      const created = await createConsoleWorkspace(orgUuid, input);
      queryClient.setQueryData<Workspace[]>(["console", "workspaces", orgUuid], (current) => {
        const existing = current ?? [];
        if (existing.some((workspace) => workspace.id === created.id)) {
          return existing.map((workspace) => (workspace.id === created.id ? created : workspace));
        }
        return [...existing, created];
      });
      setPreferredWorkspaceId(created.id);
      return created;
    },
    [orgUuid, queryClient],
  );

  const refreshWorkspaces = useCallback(async () => {
    await workspacesQuery.refetch();
  }, [workspacesQuery]);

  const value = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid,
      workspaces,
      activeWorkspace,
      activeWorkspaceId,
      isLoading: workspacesQuery.isLoading,
      error: workspacesQuery.error,
      selectWorkspace,
      createWorkspace,
      refreshWorkspaces,
    }),
    [
      activeWorkspace,
      activeWorkspaceId,
      createWorkspace,
      orgUuid,
      refreshWorkspaces,
      selectWorkspace,
      workspaces,
      workspacesQuery.error,
      workspacesQuery.isLoading,
    ],
  );

  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

function normalizeWorkspaces(apiWorkspaces: Workspace[] = []) {
  const seen = new Set([defaultWorkspace.id]);
  const workspaces = [defaultWorkspace];
  for (const workspace of apiWorkspaces) {
    const id = workspace.id?.trim();
    if (!id || seen.has(id) || workspace.name.trim().toLowerCase() === defaultWorkspace.id) {
      continue;
    }
    seen.add(id);
    workspaces.push({
      ...workspace,
      display_color: workspace.display_color || workspace.color || defaultWorkspace.display_color,
      color: workspace.color || workspace.display_color || defaultWorkspace.color,
    });
  }
  return workspaces;
}

function readStoredWorkspaceId() {
  if (typeof window === "undefined") {
    return defaultWorkspace.id;
  }
  try {
    return window.localStorage.getItem(activeWorkspaceStorageKey) || defaultWorkspace.id;
  } catch {
    return defaultWorkspace.id;
  }
}

function writeStoredWorkspaceId(workspaceId: string) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(activeWorkspaceStorageKey, workspaceId || defaultWorkspace.id);
  } catch {
    // Some embedded browser/test environments can deny storage access.
  }
}
