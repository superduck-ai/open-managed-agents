import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { cancelScopeRequests, getScopeSignal, onApiAuthFailure, setConsoleRequestContext } from '../api/client';
import { PlatformAdminClient } from '../api/platform-admin';
import { loadWorkspaceLink, workspaceLinkError } from './workspace-link';
import { AuthContext, useAuth } from '../auth/context';
import { fetchBootstrap, type AuthAccount, type OrganizationAccess } from '../auth/api';
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
type ScopeAttempt = { kind: 'organization'; orgUuid?: string } | { kind: 'workspace'; workspaceId: string };

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
  const [platformClient] = useState(() => new PlatformAdminClient(() => undefined));
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
      attempted.current = { kind: 'organization', orgUuid: requested };
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

  const enterOrganization = useCallback(
    async (orgUuid: string, restore?: PlatformScope) => {
      const current = scopeRef.current;
      attempted.current = { kind: 'organization', orgUuid };
      const account = authRef.current.account;
      if (!account) throw new Error('Authentication required');
      const platform = restore ??
        current.platform ?? {
          accountUuid: account.uuid,
          targetOrganizationUuid: orgUuid,
        };
      const ticket = ++generation.current;
      setSwitching(true);
      setError(null);
      cancelScopeRequests();
      setConsoleRequestContext({});
      await queryClient.cancelQueries({ predicate: isBusinessQuery });
      queryClient.removeQueries({ predicate: isBusinessQuery });
      await queryClient.cancelQueries({ queryKey: ['auth', 'bootstrap'] });
      try {
        const bootstrap = await fetchBootstrap({ organizationUuid: orgUuid });
        if (ticket !== generation.current) return false;
        const access = bootstrap.organization_access;
        if (bootstrap.account?.uuid !== account.uuid || !isPlatformOrganizationAccess(access, orgUuid)) {
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
          throw new Error('Platform organization access denied');
        }
        const workspaces = await listConsoleWorkspaces(orgUuid);
        if (ticket !== generation.current) return false;
        const selected = chooseWorkspace(workspaces, restore?.targetWorkspaceId ?? '');
        if (!selected) throw new Error('Default workspace unavailable');
        return await installScope(
          {
            orgUuid,
            workspaces,
            activeWorkspaceId: selected.id,
            access,
            account: bootstrap.account,
            csrfToken: bootstrap.csrf_token,
            platform: { ...platform, targetOrganizationUuid: orgUuid },
          },
          ticket,
          restore?.targetWorkspaceId !== 'default',
        );
      } catch (cause) {
        if (ticket !== generation.current) return false;
        if (restore) {
          setScope({ ...emptyScope, platform: restore });
          setError(cause);
        } else
          setConsoleRequestContext({
            organizationUuid: current.orgUuid,
            workspaceId: current.activeWorkspaceId,
            csrfToken: current.csrfToken ?? authRef.current.csrfToken,
          });
        setSwitching(false);
        throw cause;
      }
    },
    [installScope, queryClient, switchOrganization],
  );

  const openWorkspaceLink = useCallback(
    async (workspaceId: string) => {
      const account = authRef.current.account;
      if (!account) return false;
      const ticket = ++generation.current;
      const savedPlatform = readPlatformScope(account.uuid);
      attempted.current = { kind: 'workspace', workspaceId };
      recovery.current = 'idle';
      setSwitching(true);
      setError(null);
      cancelScopeRequests();
      const signal = getScopeSignal();
      setConsoleRequestContext({});
      scopeRef.current = emptyScope;
      setScope(emptyScope);
      await queryClient.cancelQueries({ predicate: isBusinessQuery });
      if (ticket !== generation.current) return false;
      queryClient.removeQueries({ predicate: isBusinessQuery });
      try {
        const next = await loadWorkspaceLink(platformClient, workspaceId, account.uuid, signal);
        if (ticket !== generation.current) return false;
        const member = account.memberships?.some((membership) => membership.organization?.uuid === next.orgUuid);
        const platformAccess = isPlatformOrganizationAccess(next.access, next.orgUuid);
        if (!member && !platformAccess) throw workspaceLinkError(403);
        const platform =
          platformAccess && (!member || savedPlatform?.targetOrganizationUuid === next.orgUuid)
            ? { accountUuid: account.uuid, targetOrganizationUuid: next.orgUuid, targetWorkspaceId: workspaceId }
            : undefined;
        clearPlatformScope();
        return await installScope({ ...next, platform }, ticket, false);
      } catch (cause) {
        if (ticket !== generation.current) return false;
        recovery.current = 'failed';
        setError(cause);
        setSwitching(false);
        return false;
      }
    },
    [installScope, platformClient, queryClient],
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
      if (explicitPlatformTarget) void openWorkspaceLink(workspaceId);
      else if (platform) void enterOrganization(platform.targetOrganizationUuid, platform).catch(() => undefined);
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
  }, [accountUuid, currentWorkspaceTarget, switchOrganization, enterOrganization, openWorkspaceLink]);

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
        enterOrganization,
        exitOrganization,
        memberships: auth.account?.memberships ?? [],
        orgUuid: scope.orgUuid,
        switching,
        error,
        switchOrganization,
        retry: () =>
          scope.platform
            ? enterOrganization(scope.platform.targetOrganizationUuid, scope.platform)
            : attempted.current.kind === 'workspace'
              ? openWorkspaceLink(attempted.current.workspaceId)
              : switchOrganization(attempted.current.orgUuid),
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
