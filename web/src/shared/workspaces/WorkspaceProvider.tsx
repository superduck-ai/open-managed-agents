import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { cancelScopeRequests, getScopeSignal, onApiAuthFailure, setConsoleRequestContext } from '../api/client';
import { fetchWorkspaceBootstrap, workspaceLinkError } from './workspace-link';
import { AuthContext, useAuth } from '../auth/context';
import { fetchBootstrap, type AuthAccount, type BootstrapResponse, type OrganizationAccess } from '../auth/api';
import {
  readPlatformScope,
  savePlatformScope,
  clearPlatformScope,
  isPlatformOrganizationAccess,
  type PlatformScope,
} from '../organizations/platform-scope';
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
  scopeAccountForSwitch,
  workspacePreference,
  workspaceTargetChanged,
} from '../organizations/scope';

type Scope = {
  orgUuid?: string;
  workspaces: Workspace[];
  activeWorkspaceId: string;
  access?: OrganizationAccess | null;
  account?: AuthAccount;
  csrfToken?: string;
  platform?: PlatformScope;
};
type ScopeAttempt =
  | { kind: 'organization'; orgUuid?: string; workspaceId?: string; navigate?: boolean }
  | { kind: 'workspace'; workspaceId: string };

type PlatformTarget =
  { kind: 'organization'; orgUuid: string; restore?: PlatformScope } | { kind: 'workspace'; workspaceId: string };

function platformBootstrapAccess(target: PlatformTarget, bootstrap: BootstrapResponse, accountUuid: string) {
  const { account, organization_access: access } = bootstrap;
  const orgUuid = target.kind === 'organization' ? target.orgUuid : access?.organization.uuid;
  if (!orgUuid || account?.uuid !== accountUuid || !isPlatformOrganizationAccess(access, orgUuid)) return;
  return { account, access };
}

function selectPlatformScope(
  target: PlatformTarget,
  account: AuthAccount,
  access: OrganizationAccess,
  workspaces: Workspace[],
): Scope {
  const orgUuid = access.organization.uuid;
  const selected =
    target.kind === 'workspace'
      ? workspaces.find((item) => item.id === target.workspaceId && !item.archived_at)
      : chooseWorkspace(workspaces, target.restore?.targetWorkspaceId ?? '');
  if (!selected) throw workspaceLinkError(404);
  const memberLink =
    target.kind === 'workspace' && account.memberships?.some((item) => item.organization?.uuid === orgUuid);
  const platform =
    memberLink && readPlatformScope(account.uuid)?.targetOrganizationUuid !== orgUuid
      ? undefined
      : { accountUuid: account.uuid, targetOrganizationUuid: orgUuid };
  return { orgUuid, workspaces, activeWorkspaceId: selected.id, account, access, platform };
}

const emptyScope: Scope = { workspaces: [], activeWorkspaceId: '' };

export function WorkspaceProvider({
  children,
  navigateScope,
  currentWorkspaceTarget,
}: {
  children: ReactNode;
  navigateScope?: (workspaceId: string) => Promise<void>;
  currentWorkspaceTarget?: () => string | undefined;
}) {
  const auth = useAuth();
  const queryClient = useQueryClient();
  const [scope, setScope] = useState<Scope>(emptyScope);
  const [switching, setSwitching] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const generation = useRef(0);
  const attempted = useRef<ScopeAttempt>({ kind: 'organization' });
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
      if (navigate && next.activeWorkspaceId) await navigateScope?.(next.activeWorkspaceId);
      if (ticket !== generation.current) return false;
      const { account, csrfToken } = authRef.current;
      setConsoleRequestContext({
        organizationUuid: next.orgUuid,
        workspaceId: next.activeWorkspaceId || undefined,
        csrfToken: next.csrfToken ?? csrfToken,
      });
      if (next.platform) {
        const platform = { ...next.platform, targetWorkspaceId: next.activeWorkspaceId };
        savePlatformScope(platform);
        next = { ...next, platform };
      } else if (account && next.orgUuid) savePreference(account.uuid, next.orgUuid, next.activeWorkspaceId);
      scopeRef.current = next;
      setScope(next);
      setSwitching(false);
      return Boolean(next.orgUuid && next.activeWorkspaceId);
    },
    [navigateScope, queryClient],
  );

  const switchOrganization = useCallback(
    async (
      requested?: string,
      refresh = true,
      navigate = true,
      knownAccount?: AuthAccount | null,
      workspaceId?: string,
    ) => {
      const previous = scopeRef.current;
      clearPlatformScope();
      if (previous.platform) {
        scopeRef.current = emptyScope;
        setScope(emptyScope);
      }
      attempted.current = { kind: 'organization', orgUuid: requested, workspaceId, navigate };
      recovery.current = 'idle';
      const ticket = ++generation.current;
      setSwitching(true);
      setError(null);
      cancelScopeRequests();
      setConsoleRequestContext({});
      await queryClient.cancelQueries({ predicate: isBusinessQuery });
      try {
        const bootstrap = refresh ? await authRef.current.refresh() : undefined;
        if (ticket !== generation.current) return false;
        const account = scopeAccountForSwitch(refresh, bootstrap?.account, knownAccount, authRef.current.account);
        const orgUuid = fallbackOrganization(account, requested);
        if (!orgUuid || !account) {
          return await installScope(emptyScope, ticket, navigate);
        }
        const effective = await fetchBootstrap({ organizationUuid: orgUuid });
        if (ticket !== generation.current) return false;
        const workspaces = await listConsoleWorkspaces(orgUuid);
        const preferred = workspacePreference(account.uuid, orgUuid, workspaceId, navigate);
        const selected = chooseWorkspace(workspaces, preferred);
        const shouldNavigate = navigate || workspaceTargetChanged(workspaceId, selected?.id);
        return await installScope(
          {
            orgUuid,
            workspaces,
            activeWorkspaceId: selected?.id ?? '',
            access: effective.organization_access,
            account: effective.account ?? undefined,
            csrfToken: effective.csrf_token,
          },
          ticket,
          shouldNavigate,
        );
      } catch (cause) {
        if (ticket !== generation.current) return false;
        if (!previous.platform)
          setConsoleRequestContext({
            organizationUuid: previous.orgUuid,
            workspaceId: previous.activeWorkspaceId,
            csrfToken: previous.csrfToken ?? authRef.current.csrfToken,
          });
        recovery.current = 'failed';
        setError(cause);
        setSwitching(false);
        return false;
      }
    },
    [installScope, queryClient],
  );

  const openPlatformTarget = useCallback(
    async (target: PlatformTarget) => {
      const account = authRef.current.account;
      if (!account) throw new Error('Authentication required');
      const current = scopeRef.current;
      const link = target.kind === 'workspace';
      const restore = target.kind === 'organization' ? target.restore : undefined;
      const ticket = ++generation.current;
      attempted.current = target;
      recovery.current = 'idle';
      setSwitching(true);
      setError(null);
      cancelScopeRequests();
      const signal = getScopeSignal();
      setConsoleRequestContext({});
      if (link) {
        scopeRef.current = emptyScope;
        setScope(emptyScope);
      }
      await Promise.all([
        queryClient.cancelQueries({ predicate: isBusinessQuery }),
        queryClient.cancelQueries({ queryKey: ['auth', 'bootstrap'] }),
      ]);
      if (ticket !== generation.current) return false;
      queryClient.removeQueries({ predicate: isBusinessQuery });
      try {
        const bootstrap = link
          ? await fetchWorkspaceBootstrap(target.workspaceId, signal)
          : await fetchBootstrap({ organizationUuid: target.orgUuid });
        if (ticket !== generation.current) return false;
        const authorized = platformBootstrapAccess(target, bootstrap, account.uuid);
        if (!authorized) {
          if (restore && !current.orgUuid) {
            clearPlatformScope();
            return await switchOrganization(
              readPreference(account.uuid),
              false,
              false,
              undefined,
              restore.targetWorkspaceId,
            );
          }
          throw workspaceLinkError(403);
        }
        const workspaces = await listConsoleWorkspaces(authorized.access.organization.uuid);
        if (ticket !== generation.current) return false;
        const next = selectPlatformScope(target, authorized.account, authorized.access, workspaces);
        if (!next.platform) clearPlatformScope();
        return await installScope({ ...next, csrfToken: bootstrap.csrf_token }, ticket, !link && !restore);
      } catch (cause) {
        if (ticket !== generation.current) return false;
        setSwitching(false);
        if (link || restore) {
          if (restore) setScope({ ...emptyScope, platform: restore });
          setError(cause);
          return false;
        }
        setConsoleRequestContext({
          organizationUuid: current.orgUuid,
          workspaceId: current.activeWorkspaceId,
          csrfToken: current.csrfToken ?? authRef.current.csrfToken,
        });
        throw cause;
      }
    },
    [installScope, queryClient, switchOrganization],
  );

  const exitOrganization = useCallback(async () => {
    const ticket = ++generation.current;
    recovery.current = 'idle';
    attempted.current = { kind: 'organization' };
    cancelScopeRequests();
    setConsoleRequestContext({});
    clearPlatformScope();
    scopeRef.current = emptyScope;
    setScope(emptyScope);
    setError(null);
    setSwitching(false);
    await queryClient.cancelQueries({ predicate: isBusinessQuery });
    if (ticket !== generation.current) return false;
    queryClient.removeQueries({ predicate: isBusinessQuery });
    return true;
  }, [queryClient]);

  const accountUuid = auth.account?.uuid;
  useEffect(() => {
    recovery.current = 'idle';
    if (accountUuid) {
      const workspaceId = currentWorkspaceTarget?.();
      const explicitPlatformTarget =
        workspaceId &&
        workspaceId !== 'default' &&
        authRef.current.platformAccess?.permissions.includes('platform:access');
      const savedPlatform = readPlatformScope(accountUuid);
      const platform =
        savedPlatform && workspaceId === 'default' ? { ...savedPlatform, targetWorkspaceId: 'default' } : savedPlatform;
      if (explicitPlatformTarget) void openPlatformTarget({ kind: 'workspace', workspaceId });
      else if (platform)
        void openPlatformTarget({
          kind: 'organization',
          orgUuid: platform.targetOrganizationUuid,
          restore: platform,
        });
      else void switchOrganization(readPreference(accountUuid), false, false, undefined, workspaceId);
    } else {
      ++generation.current;
      cancelScopeRequests();
      setConsoleRequestContext({});
      setScope(emptyScope);
      setSwitching(false);
    }
    return () => {
      ++generation.current;
    };
  }, [accountUuid, currentWorkspaceTarget, switchOrganization, openPlatformTarget]);

  useEffect(() => {
    if (auth.account && !switching && !error)
      setConsoleRequestContext({
        organizationUuid: scope.orgUuid,
        workspaceId: scope.activeWorkspaceId || undefined,
        csrfToken: scope.csrfToken ?? auth.csrfToken,
      });
  }, [auth.account, auth.csrfToken, error, scope, switching]);

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
        if (scopeRef.current.platform) {
          cancelScopeRequests();
          setConsoleRequestContext({});
          recovery.current = 'failed';
          setError(new Error('Platform organization access denied'));
          return;
        }
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
            const effective = await fetchBootstrap({ organizationUuid: orgUuid });
            if (ticket !== generation.current) return false;
            const workspaces = await listConsoleWorkspaces(orgUuid);
            if (ticket !== generation.current) return;
            if (!workspaces.some((workspace) => workspace.id === current.activeWorkspaceId)) {
              await switchOrganization(orgUuid, false, true, result?.account ?? null);
            } else {
              setScope({
                ...current,
                workspaces,
                access: effective.organization_access,
                csrfToken: effective.csrf_token,
              });
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
      attempted.current = { kind: 'organization', orgUuid: current.orgUuid };
      recovery.current = 'idle';
      cancelScopeRequests();
      setSwitching(true);
      setError(null);
      void installScope({ ...current, activeWorkspaceId: workspaceId }, ticket).catch((cause) => {
        if (ticket !== generation.current) return;
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
      const ticket = generation.current;
      const created = await createConsoleWorkspace(current.orgUuid, input);
      if (ticket !== generation.current || current !== scopeRef.current) return created;
      let workspaces: Workspace[];
      try {
        workspaces = await listConsoleWorkspaces(current.orgUuid);
      } catch {
        workspaces = current.workspaces;
      }
      if (ticket !== generation.current || current !== scopeRef.current) return created;
      if (!workspaces.some((workspace) => workspace.id === created.id)) workspaces = [...workspaces, created];
      const next = { ...current, workspaces, activeWorkspaceId: created.id };
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
    const activeWorkspaceId = chooseWorkspace(workspaces, current.activeWorkspaceId)?.id ?? '';
    await installScope(
      { ...current, workspaces, activeWorkspaceId },
      ++generation.current,
      activeWorkspaceId !== current.activeWorkspaceId,
    );
  }, [installScope]);
  const activeWorkspace = scope.workspaces.find((workspace) => workspace.id === scope.activeWorkspaceId) ?? {
    id: '',
    type: 'workspace' as const,
    name: '',
  };
  const refreshScoped = useCallback(async () => {
    const current = scopeRef.current;
    if (!current.orgUuid) return authRef.current.refresh();
    const ticket = generation.current;
    if (!current.platform) await authRef.current.refresh();
    const result = await fetchBootstrap({ organizationUuid: current.orgUuid, workspaceId: current.activeWorkspaceId });
    if (ticket !== generation.current) return undefined;
    if (
      !result.account ||
      result.account.uuid !== authRef.current.account?.uuid ||
      result.organization_access?.organization.uuid !== current.orgUuid ||
      (current.platform && result.organization_access.source !== 'platform_admin')
    )
      throw new Error('Organization access denied');
    setScope({ ...current, account: result.account, access: result.organization_access, csrfToken: result.csrf_token });
    return result;
  }, []);
  const account = useMemo(() => {
    const current = scopedAccount(scope.account ?? auth.account, scope.orgUuid, activeWorkspace, scope.access);
    return (switching || error) && current ? { ...current, permissions: [] } : current;
  }, [auth.account, scope.account, scope.orgUuid, activeWorkspace, scope.access, switching, error]);
  return (
    <OrganizationContext.Provider
      value={{
        platformScope: scope.platform,
        enterOrganization: (orgUuid) => openPlatformTarget({ kind: 'organization', orgUuid }),
        exitOrganization,
        memberships: auth.account?.memberships ?? [],
        orgUuid: scope.orgUuid,
        switching,
        error,
        switchOrganization,
        retry: () =>
          attempted.current.kind === 'workspace'
            ? openPlatformTarget(attempted.current)
            : scope.platform
              ? openPlatformTarget({
                  kind: 'organization',
                  orgUuid: scope.platform.targetOrganizationUuid,
                  restore: scope.platform,
                })
              : switchOrganization(
                  attempted.current.orgUuid,
                  true,
                  attempted.current.navigate,
                  undefined,
                  attempted.current.workspaceId,
                ),
      }}
    >
      <AuthContext.Provider
        value={{
          ...auth,
          account,
          organizationAccess: switching ? null : scope.access,
          refresh: refreshScoped,
          csrfToken: scope.csrfToken ?? auth.csrfToken,
        }}
      >
        <WorkspaceContext.Provider
          value={{
            ...scope,
            activeWorkspace,
            canManageWorkspaces: canManageMembers(account, scope.orgUuid),
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
