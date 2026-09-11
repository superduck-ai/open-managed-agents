import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { cancelScopeRequests, onApiAuthFailure, setConsoleRequestContext } from '../api/client';
import { AuthContext, useAuth } from '../auth/context';
import type { AuthAccount } from '../auth/api';
import { canManageMembers } from '../permissions/members';
import { createConsoleWorkspace, listConsoleWorkspaces, type CreateWorkspaceInput, type Workspace } from './api';
import { WorkspaceContext } from './context';
import { OrganizationContext } from '../organizations/context';
import {
  chooseWorkspace,
  fallbackOrganization,
  isBusinessQuery,
  readPreference,
  savePreference,
  scopedAccount,
} from '../organizations/scope';

type Scope = { orgUuid?: string; workspaces: Workspace[]; activeWorkspaceId: string };
const emptyScope: Scope = { workspaces: [], activeWorkspaceId: '' };

export function WorkspaceProvider({
  children,
  navigateScope,
  initialWorkspaceId,
}: {
  children: ReactNode;
  navigateScope?: (workspaceId: string) => Promise<void>;
  initialWorkspaceId?: string;
}) {
  const auth = useAuth();
  const queryClient = useQueryClient();
  const [scope, setScope] = useState<Scope>(emptyScope);
  const [switching, setSwitching] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const generation = useRef(0);
  const attempted = useRef<string | undefined>(undefined);
  const recovery = useRef<'idle' | 'checking' | 'failed'>('idle');
  const authRef = useRef(auth);
  authRef.current = auth;
  const scopeRef = useRef(scope);
  scopeRef.current = scope;

  const installScope = useCallback(
    async (next: Scope, ticket: number, navigate = true) => {
      if (ticket !== generation.current) return false;
      await queryClient.cancelQueries({ predicate: isBusinessQuery });
      if (ticket !== generation.current) return false;
      queryClient.removeQueries({ predicate: isBusinessQuery });
      const { account, csrfToken } = authRef.current;
      setConsoleRequestContext({
        organizationUuid: next.orgUuid,
        workspaceId: next.activeWorkspaceId || undefined,
        csrfToken,
      });
      if (navigate && next.activeWorkspaceId) await navigateScope?.(next.activeWorkspaceId);
      if (ticket !== generation.current) return false;
      if (account && next.orgUuid) savePreference(account.uuid, next.orgUuid, next.activeWorkspaceId);
      scopeRef.current = next;
      setScope(next);
      setSwitching(false);
      return Boolean(next.orgUuid && next.activeWorkspaceId);
    },
    [navigateScope, queryClient],
  );

  const switchOrganization = useCallback(
    async (requested?: string, refresh = true, navigate = true, knownAccount?: AuthAccount | null) => {
      attempted.current = requested;
      recovery.current = 'idle';
      const ticket = ++generation.current;
      setSwitching(true);
      setError(null);
      cancelScopeRequests();
      await queryClient.cancelQueries({ predicate: isBusinessQuery });
      try {
        const bootstrap = refresh ? await authRef.current.refresh() : undefined;
        if (ticket !== generation.current) return false;
        const account = refresh
          ? (bootstrap?.account ?? null)
          : knownAccount === undefined
            ? authRef.current.account
            : knownAccount;
        const orgUuid = fallbackOrganization(account, requested);
        if (!orgUuid || !account) {
          return await installScope(emptyScope, ticket, navigate);
        }
        const workspaces = await listConsoleWorkspaces(orgUuid);
        const preferred = !navigate && initialWorkspaceId ? initialWorkspaceId : readPreference(account.uuid, orgUuid);
        const selected = chooseWorkspace(workspaces, preferred);
        return await installScope({ orgUuid, workspaces, activeWorkspaceId: selected?.id ?? '' }, ticket, navigate);
      } catch (cause) {
        if (ticket !== generation.current) return false;
        recovery.current = 'failed';
        setError(cause);
        setSwitching(false);
        return false;
      }
    },
    [initialWorkspaceId, installScope, queryClient],
  );

  const accountUuid = auth.account?.uuid;
  useEffect(() => {
    recovery.current = 'idle';
    if (accountUuid) void switchOrganization(readPreference(accountUuid), false, false);
    else {
      ++generation.current;
      cancelScopeRequests();
      setConsoleRequestContext({});
      setScope(emptyScope);
      setSwitching(false);
    }
    return () => {
      ++generation.current;
    };
  }, [accountUuid, switchOrganization]);

  useEffect(() => {
    if (!switching && !error)
      setConsoleRequestContext({
        organizationUuid: scope.orgUuid,
        workspaceId: scope.activeWorkspaceId || undefined,
        csrfToken: auth.csrfToken,
      });
  }, [auth.csrfToken, error, scope, switching]);

  useEffect(
    () =>
      onApiAuthFailure((status, context) => {
        if (
          status !== 403 ||
          !context.organizationUuid ||
          (context.workspaceId ?? '') !== scopeRef.current.activeWorkspaceId ||
          context.organizationUuid !== scopeRef.current.orgUuid ||
          recovery.current !== 'idle'
        )
          return;
        recovery.current = 'checking';
        const ticket = generation.current;
        const current = scopeRef.current;
        void (async () => {
          try {
            const result = await authRef.current.refresh();
            if (ticket !== generation.current) return;
            const orgUuid = fallbackOrganization(result?.account ?? null, current.orgUuid);
            if (orgUuid !== current.orgUuid) {
              await switchOrganization(orgUuid, false, true, result?.account ?? null);
              return;
            }
            if (!orgUuid) return;
            const workspaces = await listConsoleWorkspaces(orgUuid);
            if (ticket !== generation.current) return;
            if (!workspaces.some((workspace) => workspace.id === current.activeWorkspaceId)) {
              await switchOrganization(orgUuid, false, true, result?.account ?? null);
            } else {
              setScope({ ...current, workspaces });
            }
          } catch (cause) {
            if (ticket === generation.current) {
              recovery.current = 'failed';
              setError(cause);
            }
          } finally {
            if (ticket === generation.current && recovery.current === 'checking') recovery.current = 'idle';
          }
        })();
      }),
    [switchOrganization],
  );

  const selectWorkspace = useCallback(
    (workspaceId: string) => {
      const current = scopeRef.current;
      if (!current.workspaces.some((workspace) => workspace.id === workspaceId)) return;
      const ticket = ++generation.current;
      recovery.current = 'idle';
      cancelScopeRequests();
      setSwitching(true);
      setError(null);
      void installScope({ ...current, activeWorkspaceId: workspaceId }, ticket).catch((cause) => {
        setError(cause);
        setSwitching(false);
      });
    },
    [installScope],
  );

  const createWorkspace = useCallback(
    async (input: CreateWorkspaceInput) => {
      const current = scopeRef.current;
      if (!current.orgUuid) throw new Error('No organization is available for workspace creation.');
      const created = await createConsoleWorkspace(current.orgUuid, input);
      if (current !== scopeRef.current) throw new DOMException('Scope changed', 'AbortError');
      const next = { ...current, workspaces: [...current.workspaces, created], activeWorkspaceId: created.id };
      setSwitching(true);
      await installScope(next, ++generation.current);
      return created;
    },
    [installScope],
  );

  const refreshWorkspaces = useCallback(async () => {
    const current = scopeRef.current;
    if (!current.orgUuid) return;
    const workspaces = await listConsoleWorkspaces(current.orgUuid);
    if (current !== scopeRef.current) return;
    setScope({ ...current, workspaces });
  }, []);
  const activeWorkspace = scope.workspaces.find((workspace) => workspace.id === scope.activeWorkspaceId) ?? {
    id: '',
    type: 'workspace' as const,
    name: '',
  };
  const account = useMemo(
    () => scopedAccount(auth.account, scope.orgUuid, activeWorkspace),
    [auth.account, scope.orgUuid, activeWorkspace],
  );
  return (
    <OrganizationContext.Provider
      value={{
        memberships: auth.account?.memberships ?? [],
        orgUuid: scope.orgUuid,
        switching,
        error,
        switchOrganization,
        retry: () => switchOrganization(attempted.current),
      }}
    >
      <AuthContext.Provider value={{ ...auth, account }}>
        <WorkspaceContext.Provider
          value={{
            ...scope,
            activeWorkspace,
            canManageWorkspaces: canManageMembers(account),
            isLoading: switching,
            error,
            selectWorkspace,
            createWorkspace,
            refreshWorkspaces,
          }}
        >
          {children}
        </WorkspaceContext.Provider>
      </AuthContext.Provider>
    </OrganizationContext.Provider>
  );
}
