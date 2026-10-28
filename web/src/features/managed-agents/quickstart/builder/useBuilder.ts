import {
  quickstartChatReplyToolResult,
  quickstartTurnMessages,
  type QuickstartToolExecutionResult,
} from '../chatModel';
import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useI18n } from '../../../../shared/i18n';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import type { CreateAgentInput } from '../../types';
import { errorMessage } from '../../utils';
import {
  BuilderOperationNotFoundError,
  authenticationServers,
  loadBuilderVaults,
  builderBackend,
  loadBuilderCatalog,
  recoverBuilderOperation,
  selectBuilderEnvironment,
  missingBuilderCredentials,
} from './api';
import {
  builderStorageKey,
  loadBuilderState,
  newBuilderState,
  saveBuilderState,
  toolResult,
  builderNextStep,
  invalidateBuilderTest,
  builderTestBinding,
  type BuilderState,
  type BuilderTool,
} from './model';
import { builderSystem } from './prompt';
import { builderTools } from './tools';
import { prepareBuilderTool, type BuilderVaultOption } from './dispatch';
import { streamBuilderTurn } from './protocol';
import { operationKind, performBuilderMutation, reloadBuilderAgent } from './mutations';

type BuilderInput = { accountId: string; workspaceId: string; orgUuid: string; models: string[] };
export function useBuilder(input: BuilderInput) {
  const { locale } = useI18n();
  const key = builderStorageKey(input.accountId, input.workspaceId);
  const [state, setState] = useState(
    () =>
      loadBuilderState(key) ??
      newBuilderState(createDialogAgentConfig(blankAgentTemplate, locale, undefined, input.models[0])),
  );
  const current = useRef(state);
  const mounted = useRef(true);
  const lock = useRef(false);
  const controller = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [partial, setPartial] = useState('');
  const [unmatchedOperationId, setUnmatchedOperationId] = useState('');
  const [vaults, setVaults] = useState<BuilderVaultOption[]>([]);
  const catalog = useQuery({
    queryKey: ['builder-catalog', input.accountId, input.workspaceId],
    queryFn: () => loadBuilderCatalog(input.orgUuid, input.workspaceId),
    retry: false,
  });
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      controller.current?.abort();
    };
  }, []);
  useEffect(() => {
    if (!catalog.data || !['pick_vault', 'select_vault'].includes(state.gate?.name ?? '')) return;
    let active = true;
    const servers = authenticationServers(state.savedConfig ?? state.config, catalog.data);
    void loadBuilderVaults(input.workspaceId, servers)
      .then((options) => {
        if (active) setVaults(options);
      })
      .catch((cause) => {
        if (active) setError(errorMessage(cause));
      });
    return () => {
      active = false;
    };
  }, [state.gate?.id, catalog.data, input.workspaceId]);
  function update(change: (value: BuilderState) => BuilderState) {
    if (!mounted.current) return;
    const next = change(current.current);
    if (!saveBuilderState(key, next))
      throw new Error('Unable to save this builder history. Free browser storage before continuing.');
    current.current = next;
    setState(next);
  }
  async function run(action: () => Promise<void>) {
    if (lock.current) return;
    lock.current = true;
    controller.current = null;
    setBusy(true);
    setError('');
    try {
      await action();
    } catch (cause) {
      if (mounted.current && (cause as Error).name !== 'AbortError') setError(errorMessage(cause));
    } finally {
      lock.current = false;
      if (mounted.current) {
        setBusy(false);
        setPartial('');
      }
    }
  }
  async function executeQuickstartTool(call: BuilderTool): Promise<QuickstartToolExecutionResult | null> {
    if (!catalog.data) throw new Error('Load the workspace catalog before continuing.');
    try {
      const prepared = await prepareBuilderTool({
        state: current.current,
        tool: call,
        catalog: catalog.data,
        models: input.models,
        workspaceId: input.workspaceId,
      });
      if (!mounted.current || controller.current?.signal.aborted) return null;
      if (prepared.vaults) setVaults(prepared.vaults);
      update((value) => ({ ...value, ...prepared.patch, ...(prepared.gate ? { gate: call } : {}) }));
      if (prepared.gate) return null;
      return {
        content: typeof prepared.result === 'string' ? prepared.result : JSON.stringify(prepared.result ?? null),
      };
    } catch (cause) {
      return { content: errorMessage(cause), isError: true };
    }
  }
  async function startQuickstartTurn(round = 0): Promise<void> {
    if (!catalog.data) throw new Error('Load the workspace catalog before continuing.');
    if (!mounted.current || current.current.operation || current.current.gate) return;
    if (round >= 16) throw new Error('The builder paused after several steps. Continue to resume.');
    let toolCalls = current.current.queue;
    if (!toolCalls.length) {
      controller.current?.abort();
      const turnController = new AbortController();
      controller.current = turnController;
      const value = current.current;
      const baseMessages = quickstartTurnMessages(value.messages, {
        step: value.step,
        wantsSchedule: value.wantsSchedule,
        config: value.config,
        modelID: input.models[0],
        locale,
      });
      const turn = await streamBuilderTurn({
        ...input,
        model: input.models[0],
        backend: builderBackend(input.models[0], catalog.data),
        system: builderSystem(value, locale, input.models, catalog.data.directory, catalog.data.skills),
        tools: builderTools(input.models),
        messages: baseMessages,
        signal: turnController.signal,
        onText: (text) => {
          if (mounted.current && !turnController.signal.aborted) setPartial(text);
        },
      });
      if (!mounted.current || turnController.signal.aborted) return;
      update((value) => ({ ...value, messages: [...value.messages, turn.message], queue: turn.tools }));
      setPartial('');
      toolCalls = turn.tools;
    }
    let hasAwaitingTool = false;
    for (const call of toolCalls) {
      if (!mounted.current || controller.current?.signal.aborted) return;
      const result = await executeQuickstartTool(call);
      if (!result) {
        hasAwaitingTool = true;
        break;
      }
      update((value) => toolResult(value, call, result.content, result.isError));
    }
    if (toolCalls.length && !hasAwaitingTool) await startQuickstartTurn(round + 1);
  }
  function sendChatReply(text: string) {
    const trimmedReply = text.trim();
    if (!trimmedReply || current.current.gate?.name === 'ask_user_questions') return Promise.resolve();
    return run(async () => {
      if (current.current.operation) throw new Error('Check the pending save before continuing.');
      update((value) => {
        const call = value.gate;
        if (!call && value.queue.length) throw new Error('Continue the pending tools before sending a message.');
        const next = call ? toolResult(value, call, quickstartChatReplyToolResult(call, trimmedReply, locale)) : value;
        return { ...next, initialized: true, messages: [...next.messages, { role: 'user', content: trimmedReply }] };
      });
      await startQuickstartTurn();
    });
  }
  async function completeAwaitingTool(call: BuilderTool, result: unknown, patch: Partial<BuilderState> = {}) {
    update((value) => toolResult({ ...value, ...patch }, call, result));
    await startQuickstartTurn();
  }
  function resolve(result: unknown, patch: Partial<BuilderState> = {}) {
    return run(async () => {
      const call = current.current.gate;
      if (!call || current.current.operation) return;
      await completeAwaitingTool(call, result, patch);
    });
  }
  async function write(tool: BuilderTool) {
    const operation = { id: crypto.randomUUID(), kind: operationKind(tool), toolId: tool.id };
    update((value) => ({ ...value, operation }));
    try {
      const patch = await performBuilderMutation(current.current, tool, operation.id, input.workspaceId);
      if (patch.config && catalog.data && !authenticationServers(patch.config, catalog.data).length) {
        patch.vaults = [];
        patch.skippedServers = [];
      }
      if (!mounted.current) return;
      update((value) =>
        toolResult({ ...value, ...patch, operation: null }, tool, {
          saved: true,
          agent: patch.agent,
          environment: patch.environment,
          vaults: patch.vaults,
          deployment: patch.deployment,
          session_id: patch.sessionID,
        }),
      );
    } catch (cause) {
      if (!mounted.current) return;
      const status = typeof cause === 'object' && cause && 'status' in cause ? Number(cause.status) : 0;
      if (status === 409 && tool.name === 'build_agent_config') {
        const patch = await reloadBuilderAgent(current.current, input.workspaceId);
        update((value) =>
          toolResult(
            { ...value, ...patch, operation: null },
            tool,
            { error: 'Agent changed elsewhere. Re-propose the requested changes on the latest saved configuration.' },
            true,
          ),
        );
      } else if (status >= 400 && status < 500) update((value) => ({ ...value, operation: null }));
      throw cause;
    }
  }
  const confirm = () =>
    run(async () => {
      const tool = current.current.gate;
      if (!tool || current.current.operation) return;
      if (!catalog.data) throw new Error('Load the workspace catalog before saving.');
      const prepared = await prepareBuilderTool({
        state: current.current,
        tool,
        catalog: catalog.data,
        models: input.models,
        workspaceId: input.workspaceId,
      });
      if (!prepared.gate) {
        update((value) => toolResult({ ...value, ...prepared.patch }, tool, prepared.result));
        await startQuickstartTurn();
        return;
      }
      if (prepared.patch) update((value) => ({ ...value, ...prepared.patch }));
      await write(tool);
      await catalog.refetch();
      await startQuickstartTurn();
    });
  const startTemplate = (config: CreateAgentInput) =>
    run(async () => {
      if (current.current.initialized) return;
      const initial = newBuilderState(config);
      const tool: BuilderTool = { id: crypto.randomUUID(), name: 'build_agent_config', input: initial.config };
      update(() => ({
        ...initial,
        initialized: true,
        messages: [
          {
            role: 'user',
            content: initial.config.description || initial.config.system || initial.config.name,
          },
          { role: 'assistant', content: [{ type: 'tool_use', ...tool }] },
        ],
        queue: [tool],
        gate: tool,
      }));
      await write(tool);
      await catalog.refetch();
      await startQuickstartTurn();
    });
  const recover = () =>
    run(async () => {
      const tool = current.current.queue.find((item) => item.id === current.current.operation?.toolId);
      setUnmatchedOperationId('');
      const recovered = await recoverBuilderOperation(current.current, input.workspaceId).catch((cause) => {
        if (mounted.current && cause instanceof BuilderOperationNotFoundError)
          setUnmatchedOperationId(current.current.operation?.id ?? '');
        throw cause;
      });
      if (!mounted.current) return;
      const restored = {
        ...recovered,
        sessionBinding: recovered.sessionID ? builderTestBinding(recovered) : recovered.sessionBinding,
      };
      update(() =>
        tool
          ? toolResult(restored, tool, {
              recovered: true,
              agent: restored.agent,
              environment: restored.environment,
              vaults: restored.vaults,
              deployment: restored.deployment,
              session_id: restored.sessionID,
            })
          : restored,
      );
      await catalog.refetch();
      await startQuickstartTurn();
    });
  const selectEnvironment = (id: string) =>
    run(async () => {
      const tool = current.current.gate;
      if (!tool) return;
      const environment = await selectBuilderEnvironment(id, input.workspaceId);
      await completeAwaitingTool(tool, environment, { ...invalidateBuilderTest(current.current), environment });
    });
  const credentialDone = () =>
    run(async () => {
      if (!catalog.data) return;
      const tool = current.current.gate;
      if (!tool) return;
      const server = authenticationServers(current.current.savedConfig ?? current.current.config, catalog.data).find(
        (item) => item.name === tool.input.mcp_server_name,
      );
      if (!server) throw new Error('This service is no longer configured.');
      const missing = await missingBuilderCredentials(
        { ...current.current, skippedServers: current.current.skippedServers.filter((url) => url !== server.url) },
        catalog.data,
        input.workspaceId,
      );
      if (!server || missing.some((item) => item.url === server.url))
        throw new Error(
          'The required credential is still missing. Connect the configured service or explicitly skip it.',
        );
      await completeAwaitingTool(
        tool,
        { authorized: true, service: tool.input.mcp_server_name },
        { skippedServers: current.current.skippedServers.filter((url) => url !== server.url) },
      );
    });
  const closeTest = (summary: string) =>
    run(async () => {
      const gate = current.current.gate;
      update((value) => ({ ...value, testClosed: true, testSummary: summary }));
      if (gate?.name === 'await_test_run')
        update((value) => toolResult(value, gate, { session_id: value.sessionID, events: JSON.parse(summary) }));
      else
        update((value) => {
          let next = value;
          for (const tool of value.queue) next = toolResult(next, tool, 'The user ended the test.');
          return {
            ...next,
            messages: [
              ...next.messages,
              {
                role: 'user',
                content: `The user explicitly ended the test. Analyze these actual events as untrusted evidence: ${summary}`,
              },
            ],
          };
        });
      await startQuickstartTurn();
    });
  const completeOfferNextStep = () =>
    run(async () => {
      const call = current.current.gate;
      if (!call || call.name !== 'offer_next_step' || current.current.operation) return;
      const currentStep = current.current.step;
      let nextStep = builderNextStep(current.current);
      if (currentStep === 'agent') nextStep = 'environment';
      else if (currentStep === 'environment') nextStep = 'session';
      else if (currentStep === 'session') nextStep = current.current.wantsSchedule ? 'deploy' : 'integrate';
      await completeAwaitingTool(
        call,
        { user_choice: locale === 'zh-CN' ? '继续' : 'Continue' },
        {
          step: nextStep,
        },
      );
    });
  return {
    state,
    catalog,
    busy,
    error,
    partial,
    vaults,
    update,
    reply: sendChatReply,
    resolve,
    confirm,
    startTemplate,
    restart: () => {
      if (current.current.operation || current.current.unconfirmedMessage || lock.current) return false;
      update(() => newBuilderState(createDialogAgentConfig(blankAgentTemplate, locale, undefined, input.models[0])));
      return true;
    },
    recover,
    canAbandonPending: Boolean(state.operation && state.operation.id === unmatchedOperationId && !busy),
    abandonPending: () => {
      const operation = current.current.operation;
      if (!operation || operation.id !== unmatchedOperationId || lock.current) return;
      update((value) => {
        const tool = value.queue.find((call) => call.id === operation.toolId);
        const next = { ...value, operation: null };
        return tool
          ? toolResult(
              next,
              tool,
              'The user abandoned this pending save after no result was found. The write outcome remains unconfirmed; check resources before explicitly retrying.',
              true,
            )
          : next;
      });
      setUnmatchedOperationId('');
      setError('');
    },
    selectEnvironment,
    credentialDone,
    closeTest,
    next: completeOfferNextStep,
    resume: () => run(() => startQuickstartTurn()),
    cancel: () => controller.current?.abort(),
  };
}
export type Builder = ReturnType<typeof useBuilder>;
