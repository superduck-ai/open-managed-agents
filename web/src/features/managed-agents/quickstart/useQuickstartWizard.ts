import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { Locale } from '../../../shared/i18n';
import type { AgentModelOption } from '../agents/create-dialog-model';
import { createAgent } from '../api';
import type { AgentApiResponse, EnvironmentApiResponse } from '../types';
import { errorMessage } from '../utils';
import { quickstartCopy } from './copy';
import {
  findQuickstartSession,
  loadQuickstartResources,
  saveQuickstartEnvironment,
  saveQuickstartSession,
} from './api';
import {
  quickstartAgentBody,
  quickstartBinding,
  quickstartDraft,
  quickstartSavedDraft,
  type QuickstartDraft,
  type QuickstartScenarioID,
} from './model';
import {
  chooseQuickstartScenario,
  initialQuickstartProgress,
  loadQuickstartProgress,
  storeQuickstartProgress,
  type QuickstartProgress,
} from './progress';

export function useQuickstartWizard(
  workspaceID: string,
  accountID: string,
  locale: Locale,
  models: AgentModelOption[],
) {
  const modelID = models[0]?.id ?? '';
  const storageKey = `oma-quickstart-v1:${accountID}:${workspaceID}`;
  const text = quickstartCopy(locale);
  const client = useQueryClient();
  const resourceKey = ['quickstart-resources', accountID, workspaceID];
  const resources = useQuery({
    queryKey: resourceKey,
    queryFn: () => loadQuickstartResources(workspaceID),
    retry: false,
  });
  const [progress, setProgress] = useState(
    () => loadQuickstartProgress(storageKey) ?? initialQuickstartProgress(quickstartDraft('hello', locale, modelID)),
  );
  const current = useRef(progress);
  const lock = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [storageAvailable, setStorageAvailable] = useState(true);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const update = useCallback(
    (change: (value: QuickstartProgress) => QuickstartProgress) => {
      if (!mounted.current) return;
      const next = change(current.current);
      current.current = next;
      setStorageAvailable(storeQuickstartProgress(storageKey, next));
      setProgress(next);
    },
    [storageKey],
  );
  const agents = resources.data?.agents ?? [];
  const environments = resources.data?.environments ?? [];
  const activeEnvironments = environments.filter((item) => !item.archived_at);
  const agent = agents.find((item) => item.id === progress.agentID) ?? null;
  const environment = activeEnvironments.find((item) => item.id === progress.environmentID) ?? null;
  const draft = agent ? quickstartSavedDraft(agent) : progress.drafts[progress.scenarioID];
  const candidates = agents.filter((item) => item.name.trim() === draft.name.trim());
  const namedEnvironment =
    progress.environmentID === 'new'
      ? environments.find((item) => item.name.trim() === progress.environmentName.trim())
      : undefined;
  const modelAvailable = models.some((model) => model.id === draft.modelID);
  const agentValid = Boolean((agent || (draft.name.trim() && draft.system.trim())) && modelAvailable);
  const reachableStep = agent && modelAvailable ? (environment ? 3 : 2) : 1;
  const navigationDisabled = busy || Boolean(progress.pending);
  const canNavigate = (step: number) =>
    !navigationDisabled && step >= 0 && step <= 3 && (step <= progress.step || step <= reachableStep);
  const navigate = (step: number) => {
    if (canNavigate(step)) update((value) => ({ ...value, step }));
  };

  useEffect(() => {
    if (!resources.data) return;
    if (!current.current.environmentID) {
      const active = resources.data.environments.filter((item) => !item.archived_at);
      const preferred = active.find((item) => item.name === 'Default') ?? (active.length === 1 ? active[0] : undefined);
      if (preferred) update((value) => ({ ...value, environmentID: preferred.id }));
    }
    if (
      current.current.agentID &&
      current.current.agentID !== 'new' &&
      !resources.data.agents.some((item) => item.id === current.current.agentID)
    ) {
      update((value) => ({ ...value, agentID: '', sessionID: '', step: Math.min(value.step, 1) }));
      setError(text.restoreFailed);
    }
    if (
      current.current.environmentID !== 'new' &&
      current.current.environmentID &&
      !resources.data.environments.some((item) => item.id === current.current.environmentID && !item.archived_at)
    ) {
      update((value) => ({ ...value, environmentID: '', sessionID: '', step: Math.min(value.step, 2) }));
      setError(text.restoreFailed);
    }
  }, [resources.data, text.restoreFailed, update]);

  const defaultAgent = progress.step === 1 && !progress.agentID ? candidates[0] : undefined;
  useEffect(() => {
    if (defaultAgent && !busy && !progress.pending) update((value) => ({ ...value, agentID: defaultAgent.id }));
  }, [defaultAgent, busy, progress.pending, update]);

  const addResource = (item: AgentApiResponse | EnvironmentApiResponse) => {
    client.setQueryData<Awaited<ReturnType<typeof loadQuickstartResources>>>(resourceKey, (value) => {
      if (!value) return value;
      return item.type === 'agent'
        ? { ...value, agents: [...value.agents.filter((agent) => agent.id !== item.id), item] }
        : { ...value, environments: [...value.environments.filter((environment) => environment.id !== item.id), item] };
    });
  };
  const begin = () => {
    if (lock.current) return false;
    lock.current = true;
    setBusy(true);
    setError(null);
    return true;
  };
  const end = () => {
    lock.current = false;
    if (mounted.current) setBusy(false);
  };
  const failed = (cause: unknown) => {
    const status = typeof cause === 'object' && cause && 'status' in cause ? Number(cause.status) : 0;
    if (status >= 400 && status < 500) update((value) => ({ ...value, pending: null }));
    if (mounted.current) setError(errorMessage(cause));
  };
  const saveAgent = async () => {
    if (current.current.pending || !agentValid || !begin()) return;
    try {
      if (agent) {
        update((value) => ({ ...value, step: 2 }));
        return;
      }
      const id = crypto.randomUUID();
      update((value) => ({ ...value, pending: { kind: 'agent', id } }));
      const saved = await createAgent(
        { ...quickstartAgentBody(draft), metadata: { quickstart_operation_id: id } },
        workspaceID,
      );
      addResource(saved);
      update((value) => ({ ...value, agentID: saved.id, step: 2, pending: null }));
    } catch (cause) {
      failed(cause);
    } finally {
      end();
    }
  };
  const saveEnvironment = async () => {
    if (
      current.current.pending ||
      (!environment && (progress.environmentID !== 'new' || !progress.environmentName.trim())) ||
      namedEnvironment?.archived_at ||
      !begin()
    )
      return;
    try {
      const existing = environment ?? namedEnvironment;
      if (existing?.archived_at) throw new Error(text.archivedName);
      if (existing) {
        update((value) => ({ ...value, environmentID: existing.id, step: 3 }));
        return;
      }
      const id = crypto.randomUUID();
      update((value) => ({ ...value, pending: { kind: 'environment', id } }));
      const saved = await saveQuickstartEnvironment(progress.environmentName, id, workspaceID);
      addResource(saved);
      update((value) => ({ ...value, environmentID: saved.id, step: 3, pending: null }));
    } catch (cause) {
      failed(cause);
    } finally {
      end();
    }
  };
  const recover = async () => {
    const pending = current.current.pending;
    if (!pending || !begin()) return;
    try {
      const loaded = await loadQuickstartResources(workspaceID);
      client.setQueryData(resourceKey, loaded);
      const results =
        pending.kind === 'session'
          ? await findQuickstartSession(pending.id, workspaceID)
          : (pending.kind === 'agent' ? loaded.agents : loaded.environments).filter(
              (item) => item.metadata?.quickstart_operation_id === pending.id,
            );
      if (results.length !== 1) {
        setError(results.length ? text.duplicate : text.notFound);
        return;
      }
      const saved = results[0];
      update((value) => ({
        ...value,
        pending: null,
        ...(pending.kind === 'agent'
          ? { agentID: saved.id, step: 2 }
          : pending.kind === 'environment'
            ? { environmentID: saved.id, step: 3 }
            : { sessionID: saved.id }),
      }));
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      end();
    }
  };
  const createSession = async () => {
    if (!agent || !environment || current.current.pending || !begin()) return null;
    const binding = quickstartBinding(agent, environment.id);
    const id = crypto.randomUUID();
    update((value) => ({
      ...value,
      pending: { kind: 'session', id },
      sessionBinding: binding,
      sessionID: '',
      unconfirmedMessage: '',
    }));
    try {
      const session = await saveQuickstartSession(agent, environment.id, id, workspaceID);
      if (!mounted.current) return null;
      client.setQueryData(['quickstart-session', accountID, workspaceID, session.id], session);
      update((value) => ({ ...value, sessionID: session.id, pending: null }));
      return session;
    } catch (cause) {
      failed(cause);
      return null;
    } finally {
      end();
    }
  };
  const chooseScenario = (scenarioID: QuickstartScenarioID) =>
    update((value) => chooseQuickstartScenario(value, scenarioID, quickstartDraft(scenarioID, locale, modelID)));
  const editDraft = (change: Partial<QuickstartDraft>) =>
    update((value) => ({
      ...value,
      drafts: { ...value.drafts, [value.scenarioID]: { ...value.drafts[value.scenarioID], ...change } },
    }));
  return {
    progress,
    modelAvailable,
    agentValid,
    reachableStep,
    navigationDisabled,
    canNavigate,
    navigate,
    update,
    resources,
    agent,
    environment,
    draft,
    candidates,
    activeEnvironments,
    namedEnvironment,
    busy,
    error,
    storageAvailable,
    saveAgent,
    saveEnvironment,
    createSession,
    recover,
    chooseScenario,
    editDraft,
  };
}

export type QuickstartWizard = ReturnType<typeof useQuickstartWizard>;
