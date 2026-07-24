import { type Locale, useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
  type ResizablePanelHandle,
} from '../../../shared/ui/resizable';
import { toast } from '../../../shared/ui/sonner';
import { useWorkspace } from '../../../shared/workspaces/context';
import {
  buildInitialQuickstartMessage,
  buildPlatformQuickstartRequest,
  buildQuickstartTurnContextText,
  type QuickstartMessage,
  type QuickstartStep,
} from './platformQuickstartRequest';
import { quickstartToolResultText } from './quickstartPromptText';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  agentTemplates,
  blankAgentTemplate,
  createDialogAgentConfig,
  quickstartBuildAgentConfigInput,
} from '../agentConfig';
import { ManagedErrorAlert } from '../components/common';
import {
  createAgent,
  createQuickstartDeployment,
  createQuickstartEnvironment,
  createQuickstartSession,
  createQuickstartVault,
  createQuickstartVaultCredential,
  interruptQuickstartSession,
  listManagedEntities,
  postQuickstartProxyStream,
  postQuickstartSessionMessage,
} from '../api';
import { templateBody, templateSearchText } from '../labels';
import { useEffectiveModelMappings } from '../modelMappings';
import {
  type AgentApiResponse,
  type AgentPanelTab,
  type AgentTemplate,
  type CodeFormat,
  type CreateAgentInput,
  type EnvironmentApiResponse,
  type QuickstartChatItem,
  type QuickstartToolCall,
  type QuickstartToolExecutionResult,
  type SessionApiResponse,
} from '../types';
import { agentDetailHref, errorMessage, parseToolInput, titleCase, toRecord } from '../utils';
import {
  appendQuickstartStatus,
  awaitingQuickstartToolCalls,
  BrowseTemplatesPanel,
  cleanQuickstartAssistantText,
  CreatedAgentConfigPanel,
  hasAwaitingQuickstartQuestionSet,
  InitialPromptPane,
  QuickstartChatPane,
  quickstartChatReplyToolResult,
  quickstartItemId,
  quickstartVaultIdsFromInput,
  TemplateDetailPanel,
  toolResultBlock,
  updateQuickstartMessage,
  updateQuickstartTool,
} from './components';
import { QuickstartHeader } from './QuickstartHeader';

export { quickstartSteps } from './steps';

export const quickstartPrimaryPaneMinWidth = 360;

export const quickstartInspectorPaneMinWidth = 440;

export const quickstartInspectorPaneDefaultWidth = 720;

export function clampQuickstartInspectorPaneWidth(width: number, containerWidth: number) {
  if (containerWidth <= 0) {
    return Math.round(width);
  }
  const maxWidth = Math.max(quickstartInspectorPaneMinWidth, containerWidth - quickstartPrimaryPaneMinWidth);
  return Math.min(Math.max(Math.round(width), quickstartInspectorPaneMinWidth), maxWidth);
}

export function AgentQuickstartPage() {
  const { msg } = useI18n();
  const { orgUuid } = useWorkspace();
  const modelMappingsQuery = useEffectiveModelMappings(orgUuid);
  if (orgUuid && modelMappingsQuery.isPending) {
    return <section aria-busy="true" className="h-[calc(100vh-48px)] min-h-[650px]" />;
  }
  if (orgUuid && modelMappingsQuery.isError) {
    return (
      <section className="flex h-[calc(100vh-48px)] min-h-[650px] items-center justify-center px-6">
        <div className="flex w-full max-w-xl flex-col gap-3">
          <ManagedErrorAlert>
            {msg(
              'managedAgents.models.loadFailedBody',
              'Retry before creating an agent so its displayed and saved model IDs stay consistent.',
            )}
          </ManagedErrorAlert>
          <Button type="button" className="self-start" onClick={() => void modelMappingsQuery.refetch()}>
            {msg('common.retry', 'Retry')}
          </Button>
        </div>
      </section>
    );
  }
  return <AgentQuickstartContent modelMappings={modelMappingsQuery.data ?? {}} />;
}

function AgentQuickstartContent({ modelMappings }: { modelMappings: Record<string, string> }) {
  const { msg, locale } = useI18n();
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const [query, setQuery] = useState('');
  const [prompt, setPrompt] = useState('');
  const [reply, setReply] = useState('');
  const [detailTemplateId, setDetailTemplateId] = useState<string | null>(null);
  const [createdTemplateId, setCreatedTemplateId] = useState<string | null>(null);
  const [createdAgent, setCreatedAgent] = useState<AgentApiResponse | null>(null);
  const [createdAgentConfig, setCreatedAgentConfig] = useState<CreateAgentInput | null>(null);
  const [chatItems, setChatItems] = useState<QuickstartChatItem[]>([]);
  const [creatingTemplateId, setCreatingTemplateId] = useState<string | null>(null);
  const [isChatStreaming, setIsChatStreaming] = useState(false);
  const [chatError, setChatError] = useState<string | null>(null);
  const [format, setFormat] = useState<CodeFormat>('YAML');
  const [agentTab, setAgentTab] = useState<AgentPanelTab>('config');
  const [activeStep, setActiveStep] = useState(0);
  const [selectedEnvironment, setSelectedEnvironment] = useState<EnvironmentApiResponse | null>(null);
  const [selectedVaultIds, setSelectedVaultIds] = useState<string[]>([]);
  const [session, setSession] = useState<SessionApiResponse | null>(null);
  const [testRunStopped, setTestRunStopped] = useState(false);
  const [testRunMessage, setTestRunMessage] = useState('');
  const [deploymentSchedulePlanned, setDeploymentSchedulePlanned] = useState(false);
  const [quickstartInspectorPaneWidth, setQuickstartInspectorPaneWidth] = useState(quickstartInspectorPaneDefaultWidth);
  const [quickstartGridWidth, setQuickstartGridWidth] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const quickstartGridRef = useRef<HTMLDivElement>(null);
  const quickstartInspectorPanelRef = useRef<ResizablePanelHandle | null>(null);
  const conversationMessagesRef = useRef<QuickstartMessage[]>([]);
  const activeStepRef = useRef<QuickstartStep>('agent');
  const createdAgentRef = useRef<AgentApiResponse | null>(null);
  const createdAgentConfigRef = useRef<CreateAgentInput | null>(null);
  const selectedEnvironmentRef = useRef<EnvironmentApiResponse | null>(null);
  const selectedVaultIdsRef = useRef<string[]>([]);
  const sessionRef = useRef<SessionApiResponse | null>(null);
  const deploymentSchedulePlannedRef = useRef(false);
  const initialAgentDescriptionRef = useRef<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  // Keep only the model-facing Builder conversation in one language. UI labels,
  // template previews/configuration, and integration scaffolds continue following the
  // live console locale while a run is in progress.
  const promptLocaleRef = useRef<Locale>('en');
  const quickstartResultText = () => quickstartToolResultText(promptLocaleRef.current);

  const visibleTemplates = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) {
      return agentTemplates;
    }
    return agentTemplates.filter((template) => templateSearchText(template, msg).toLowerCase().includes(normalized));
  }, [msg, query]);

  const detailTemplate = agentTemplates.find((template) => template.id === detailTemplateId) ?? null;
  const createdTemplate = agentTemplates.find((template) => template.id === createdTemplateId) ?? null;
  const quickstartInspectorPaneMaxWidth = quickstartGridWidth
    ? Math.max(quickstartInspectorPaneMinWidth, quickstartGridWidth - quickstartPrimaryPaneMinWidth)
    : undefined;

  useEffect(() => {
    createdAgentRef.current = createdAgent;
  }, [createdAgent]);

  useEffect(() => {
    createdAgentConfigRef.current = createdAgentConfig;
  }, [createdAgentConfig]);

  useEffect(() => {
    selectedEnvironmentRef.current = selectedEnvironment;
  }, [selectedEnvironment]);

  useEffect(() => {
    selectedVaultIdsRef.current = selectedVaultIds;
  }, [selectedVaultIds]);

  useEffect(() => {
    sessionRef.current = session;
  }, [session]);

  useEffect(() => {
    deploymentSchedulePlannedRef.current = deploymentSchedulePlanned;
  }, [deploymentSchedulePlanned]);

  useEffect(
    () => () => {
      abortRef.current?.abort();
    },
    [],
  );

  useEffect(() => {
    const grid = quickstartGridRef.current;
    if (!grid) {
      return;
    }

    const clampToGridWidth = () => {
      setQuickstartGridWidth(grid.getBoundingClientRect().width);
    };

    clampToGridWidth();
    const resizeObserver = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(clampToGridWidth) : null;
    resizeObserver?.observe(grid);
    window.addEventListener('resize', clampToGridWidth);
    return () => {
      resizeObserver?.disconnect();
      window.removeEventListener('resize', clampToGridWidth);
    };
  }, []);

  useEffect(() => {
    if (!quickstartGridWidth) {
      return;
    }
    let targetWidth = quickstartInspectorPaneWidth;
    // Start narrower layouts with equal-width work areas.
    if (quickstartInspectorPaneWidth === quickstartInspectorPaneDefaultWidth) {
      const halfWidth = Math.round(quickstartGridWidth / 2);
      if (halfWidth < quickstartInspectorPaneDefaultWidth) {
        targetWidth = halfWidth;
      }
    }
    const nextWidth = clampQuickstartInspectorPaneWidth(targetWidth, quickstartGridWidth);
    if (nextWidth === quickstartInspectorPaneWidth) {
      return;
    }
    setQuickstartInspectorPaneWidth(nextWidth);
    quickstartInspectorPanelRef.current?.resize(nextWidth);
  }, [quickstartGridWidth, quickstartInspectorPaneWidth]);

  const buildCurrentQuickstartTurnContextBlock = () => ({
    type: 'text',
    text: buildQuickstartTurnContextText({
      step: activeStepRef.current,
      deploymentSchedulePlanned: deploymentSchedulePlannedRef.current,
      agentDescription: initialAgentDescriptionRef.current,
      agentConfig: createdAgentConfigRef.current ?? {},
      locale: promptLocaleRef.current,
    }),
  });

  const buildQuickstartToolResultMessage = (blocks: Array<Record<string, unknown>>): QuickstartMessage => ({
    role: 'user',
    content: [...blocks, buildCurrentQuickstartTurnContextBlock()],
  });

  const handleTemplateClick = (template: AgentTemplate) => {
    setDetailTemplateId(template.id);
    setFormat('YAML');
  };

  const startQuickstartTurn = async (messages?: QuickstartMessage[]) => {
    const agentConfig = createdAgentConfigRef.current;
    if (!agentConfig) {
      return;
    }
    if (!orgUuid) {
      const noOrgMessage = quickstartResultText().noOrganization;
      setChatError(noOrgMessage);
      return;
    }

    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setIsChatStreaming(true);
    setChatError(null);

    const baseMessages =
      messages ??
      (conversationMessagesRef.current.length
        ? conversationMessagesRef.current
        : [
            buildInitialQuickstartMessage({
              step: activeStepRef.current,
              deploymentSchedulePlanned: deploymentSchedulePlannedRef.current,
              agentDescription: initialAgentDescriptionRef.current,
              agentConfig,
              locale: promptLocaleRef.current,
            }),
          ]);
    const requestBody = buildPlatformQuickstartRequest({
      step: activeStepRef.current,
      deploymentSchedulePlanned: deploymentSchedulePlannedRef.current,
      agentConfig,
      agentDescription: initialAgentDescriptionRef.current,
      messages: baseMessages,
      locale: promptLocaleRef.current,
    });

    const assistantItemId = quickstartItemId('assistant');
    let assistantText = '';
    let hasAssistantItem = false;
    const toolCalls: QuickstartToolCall[] = [];
    const assistantContent: Array<Record<string, unknown>> = [];
    const updateAssistantDisplay = () => {
      const displayText = cleanQuickstartAssistantText(assistantText);
      if (!displayText.trim()) {
        if (hasAssistantItem) {
          hasAssistantItem = false;
          setChatItems((current) => current.filter((item) => item.id !== assistantItemId));
        }
        return;
      }
      if (!hasAssistantItem) {
        hasAssistantItem = true;
        setChatItems((current) => [
          ...current,
          { id: assistantItemId, type: 'message', role: 'assistant', content: displayText },
        ]);
      } else {
        setChatItems((current) => updateQuickstartMessage(current, assistantItemId, displayText));
      }
    };
    let currentTool: {
      id: string;
      name: string;
      input: Record<string, unknown>;
      inputJson: string;
    } | null = null;
    try {
      await postQuickstartProxyStream({
        orgUuid,
        workspaceId: activeWorkspaceId,
        body: requestBody,
        signal: controller.signal,
        onEvent: (event) => {
          const type = typeof event.data.type === 'string' ? event.data.type : event.event;
          if (type === 'content_block_start') {
            const contentBlock = toRecord(event.data.content_block);
            if (contentBlock?.type === 'tool_use') {
              currentTool = {
                id: String(contentBlock.id ?? quickstartItemId('tool_use')),
                name: String(contentBlock.name ?? 'unknown_tool'),
                input: toRecord(contentBlock.input) ?? {},
                inputJson: '',
              };
            }
            return;
          }

          if (type === 'content_block_delta') {
            const delta = toRecord(event.data.delta);
            if (delta?.type === 'text_delta' && typeof delta.text === 'string') {
              assistantText += delta.text;
              updateAssistantDisplay();
            }
            if (delta?.type === 'input_json_delta' && typeof delta.partial_json === 'string' && currentTool) {
              currentTool.inputJson += delta.partial_json;
            }
            return;
          }

          if (type === 'content_block_stop' && currentTool) {
            const parsedInput = parseToolInput(currentTool.inputJson, currentTool.input);
            const call: QuickstartToolCall = {
              id: currentTool.id,
              name: currentTool.name,
              input: parsedInput,
              status: 'running',
            };
            toolCalls.push(call);
            assistantContent.push({ type: 'tool_use', id: call.id, name: call.name, input: call.input });
            setChatItems((current) => [...current, { id: quickstartItemId('tool'), type: 'tool', call }]);
            currentTool = null;
          }
        },
      });

      const assistantMessageText = cleanQuickstartAssistantText(assistantText);
      if (assistantMessageText.trim()) {
        assistantContent.unshift({ type: 'text', text: assistantMessageText });
      }
      conversationMessagesRef.current = [
        ...baseMessages,
        {
          role: 'assistant',
          content: assistantContent.length ? assistantContent : assistantMessageText,
        },
      ];

      const resultBlocks: Array<Record<string, unknown>> = [];
      let hasAwaitingTool = false;
      for (const call of toolCalls) {
        const result = await executeQuickstartTool(call);
        if (!result) {
          hasAwaitingTool = true;
          continue;
        }
        resultBlocks.push(toolResultBlock(call.id, result));
      }

      if (resultBlocks.length && !hasAwaitingTool) {
        const nextMessages: QuickstartMessage[] = [
          ...conversationMessagesRef.current,
          buildQuickstartToolResultMessage(resultBlocks),
        ];
        conversationMessagesRef.current = nextMessages;
        await startQuickstartTurn(nextMessages);
      }
    } catch (error) {
      if ((error as DOMException).name !== 'AbortError') {
        const message = errorMessage(error);
        setChatError(message);
      }
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = null;
      }
      setIsChatStreaming(false);
    }
  };

  const executeQuickstartTool = async (call: QuickstartToolCall): Promise<QuickstartToolExecutionResult | null> => {
    updateQuickstartTool(setChatItems, call.id, { status: 'running' });
    const t = quickstartResultText();
    try {
      switch (call.name) {
        case 'ask_user_questions':
        case 'agent_ready':
        case 'await_test_run':
          updateQuickstartTool(setChatItems, call.id, { status: 'awaiting_user' });
          return null;
        case 'build_agent_config': {
          const nextConfig = quickstartBuildAgentConfigInput(
            call.input,
            createdAgentConfigRef.current ??
              createDialogAgentConfig(blankAgentTemplate, promptLocaleRef.current, undefined, modelMappings),
            modelMappings,
          );
          setCreatedAgentConfig(nextConfig);
          createdAgentConfigRef.current = nextConfig;
          setAgentTab('config');
          updateQuickstartTool(setChatItems, call.id, { status: 'awaiting_user' });
          return null;
        }
        case 'offer_next_step':
          if (activeStepRef.current === 'environment' && !selectedEnvironmentRef.current) {
            const message = t.selectEnvironmentFirst;
            updateQuickstartTool(setChatItems, call.id, { status: 'completed', result: message });
            return {
              content: t.offerNextStepEnvHint(message),
              isError: true,
            };
          }
          if (activeStepRef.current === 'session' && !sessionRef.current) {
            const message = t.agentReadyFirst;
            updateQuickstartTool(setChatItems, call.id, { status: 'completed', result: message });
            return {
              content: t.offerNextStepSessionHint(message),
              isError: true,
            };
          }
          updateQuickstartTool(setChatItems, call.id, { status: 'awaiting_user' });
          return null;
        case 'vault_sharing_notice':
          updateQuickstartTool(setChatItems, call.id, { status: 'completed', result: t.vaultSharingNoticeShown });
          return { content: t.vaultSharingNoticeShown };
        case 'list_environments': {
          const environments = await listManagedEntities('environments', activeWorkspaceId);
          updateQuickstartTool(setChatItems, call.id, { status: 'completed', result: t.environmentsLoaded });
          return { content: JSON.stringify(environments) };
        }
        case 'create_environment': {
          const reuseEnvironmentId =
            typeof call.input.reuse_environment_id === 'string' ? call.input.reuse_environment_id.trim() : '';
          const environment = await createQuickstartEnvironment(call.input, activeWorkspaceId);
          setSelectedEnvironment(environment);
          selectedEnvironmentRef.current = environment;
          if (!reuseEnvironmentId) {
            toast.success(
              msg('managedAgents.quickstart.environmentCreatedAria', 'Environment created - {id}', {
                id: environment.id,
              }),
            );
          }
          setActiveStep(1);
          activeStepRef.current = 'environment';
          const resultText = reuseEnvironmentId
            ? t.environmentSelected(environment.id)
            : t.environmentCreated(environment.id);
          updateQuickstartTool(setChatItems, call.id, {
            status: 'awaiting_user',
            result: resultText,
          });
          return null;
        }
        case 'list_vaults': {
          const vaults = await listManagedEntities('credential-vaults', activeWorkspaceId);
          updateQuickstartTool(setChatItems, call.id, { status: 'completed', result: t.vaultsLoaded });
          return { content: JSON.stringify(vaults) };
        }
        case 'select_vault': {
          const vaultIds = quickstartVaultIdsFromInput(call.input);
          if (vaultIds.length) {
            const nextVaultIds = Array.from(new Set([...selectedVaultIdsRef.current, ...vaultIds]));
            selectedVaultIdsRef.current = nextVaultIds;
            setSelectedVaultIds(nextVaultIds);
          }
          const resultText = vaultIds.length ? t.vaultSelected(vaultIds.join(', ')) : t.noVaultSelected;
          updateQuickstartTool(setChatItems, call.id, { status: 'awaiting_user', result: resultText });
          return null;
        }
        case 'create_vault': {
          const vault = await createQuickstartVault(call.input, activeWorkspaceId);
          const nextVaultIds = Array.from(new Set([...selectedVaultIdsRef.current, vault.id]));
          selectedVaultIdsRef.current = nextVaultIds;
          setSelectedVaultIds(nextVaultIds);
          updateQuickstartTool(setChatItems, call.id, {
            status: 'completed',
            result: t.vaultCreated(vault.id),
          });
          return { content: t.vaultCreated(vault.id) };
        }
        case 'create_vault_credential': {
          updateQuickstartTool(setChatItems, call.id, { status: 'awaiting_user' });
          return null;
        }
        case 'flag_schedule_intent': {
          const wantsSchedule = Boolean(call.input.wants_schedule);
          setDeploymentSchedulePlanned(wantsSchedule);
          deploymentSchedulePlannedRef.current = wantsSchedule;
          const resultText = wantsSchedule ? t.scheduleIntentFlagged : t.scheduleIntentCleared;
          updateQuickstartTool(setChatItems, call.id, { status: 'completed', result: resultText });
          return { content: resultText };
        }
        case 'create_deployment': {
          const agent = createdAgentRef.current;
          const environmentId = selectedEnvironmentRef.current?.id;
          if (!agent || !environmentId) {
            throw new Error(t.deploymentRequiresAgentEnv);
          }
          const deployment = await createQuickstartDeployment(
            agent,
            environmentId,
            selectedVaultIdsRef.current,
            call.input,
            activeWorkspaceId,
          );
          updateQuickstartTool(setChatItems, call.id, {
            status: 'completed',
            result: t.deploymentCreated(deployment.id),
          });
          return {
            content: t.deploymentCreated(deployment.id),
          };
        }
        case 'show_integration_exits':
          setActiveStep(3);
          activeStepRef.current = 'integrate';
          updateQuickstartTool(setChatItems, call.id, { status: 'awaiting_user' });
          return null;
        case 'web_search':
          updateQuickstartTool(setChatItems, call.id, {
            status: 'completed',
            result: t.webSearchUpstream,
          });
          return { content: t.webSearchUpstream };
        default:
          updateQuickstartTool(setChatItems, call.id, { status: 'completed', result: t.toolCompleted(call.name) });
          return { content: t.toolCompleted(call.name) };
      }
    } catch (error) {
      const message = errorMessage(error);
      updateQuickstartTool(setChatItems, call.id, { status: 'failed', error: message });
      return { content: message, isError: true };
    }
  };

  const completeAwaitingTool = async (call: QuickstartToolCall, result: QuickstartToolExecutionResult) => {
    updateQuickstartTool(setChatItems, call.id, {
      status: result.isError ? 'failed' : 'completed',
      result: result.isError ? undefined : result.content,
      error: result.isError ? result.content : undefined,
    });
    const nextMessages: QuickstartMessage[] = [
      ...conversationMessagesRef.current,
      buildQuickstartToolResultMessage([toolResultBlock(call.id, result)]),
    ];
    conversationMessagesRef.current = nextMessages;
    await startQuickstartTurn(nextMessages);
  };

  const handleUseTemplate = async (template: AgentTemplate, descriptionOverride?: string | null) => {
    promptLocaleRef.current = locale;
    const config = createDialogAgentConfig(template, locale, descriptionOverride, modelMappings);
    setCreatingTemplateId(template.id);
    setChatError(null);
    try {
      const created = await createAgent(config, activeWorkspaceId);
      setCreatedTemplateId(template.id);
      setCreatedAgent(created);
      setCreatedAgentConfig(config);
      createdAgentRef.current = created;
      createdAgentConfigRef.current = config;
      setDetailTemplateId(null);
      setFormat('YAML');
      setAgentTab('config');
      setActiveStep(0);
      activeStepRef.current = 'agent';
      setSelectedEnvironment(null);
      selectedEnvironmentRef.current = null;
      setSelectedVaultIds([]);
      selectedVaultIdsRef.current = [];
      setSession(null);
      sessionRef.current = null;
      setTestRunStopped(false);
      setTestRunMessage('');
      setDeploymentSchedulePlanned(false);
      deploymentSchedulePlannedRef.current = false;
      initialAgentDescriptionRef.current = null;
      conversationMessagesRef.current = [];
      setChatItems([
        {
          id: quickstartItemId('user'),
          type: 'message',
          role: 'user',
          content: descriptionOverride?.trim() || templateBody(template, msg),
        },
        { id: quickstartItemId('create_agent_result'), type: 'create_agent_result', agentConfig: config },
      ]);
    } catch (error) {
      const message = errorMessage(error);
      setChatError(message);
      appendQuickstartStatus(setChatItems, message, 'error');
    } finally {
      setCreatingTemplateId(null);
    }
  };

  const resetQuickstart = () => {
    abortRef.current?.abort();
    setCreatedTemplateId(null);
    setCreatedAgent(null);
    setCreatedAgentConfig(null);
    createdAgentRef.current = null;
    createdAgentConfigRef.current = null;
    setDetailTemplateId(null);
    setFormat('YAML');
    setAgentTab('config');
    setActiveStep(0);
    activeStepRef.current = 'agent';
    setSelectedEnvironment(null);
    selectedEnvironmentRef.current = null;
    setSelectedVaultIds([]);
    selectedVaultIdsRef.current = [];
    setSession(null);
    sessionRef.current = null;
    setTestRunStopped(false);
    setTestRunMessage('');
    setDeploymentSchedulePlanned(false);
    deploymentSchedulePlannedRef.current = false;
    initialAgentDescriptionRef.current = null;
    conversationMessagesRef.current = [];
    setChatItems([]);
    setChatError(null);
    setReply('');
  };

  const sendChatReply = async () => {
    const trimmedReply = reply.trim();
    if (!trimmedReply || isChatStreaming || hasAwaitingQuickstartQuestionSet(chatItems)) {
      return;
    }
    setReply('');
    setChatItems((current) => [
      ...current,
      { id: quickstartItemId('user'), type: 'message', role: 'user', content: trimmedReply },
    ]);
    const awaitingTools = awaitingQuickstartToolCalls(chatItems);
    const replyMessage: QuickstartMessage = awaitingTools.length
      ? {
          role: 'user',
          content: [
            ...awaitingTools.map((call) =>
              toolResultBlock(call.id, {
                content: quickstartChatReplyToolResult(call, trimmedReply, promptLocaleRef.current),
              }),
            ),
            buildCurrentQuickstartTurnContextBlock(),
          ],
        }
      : { role: 'user', content: trimmedReply };
    if (awaitingTools.length) {
      setChatItems((current) =>
        current.map((item) =>
          item.type === 'tool' && awaitingTools.some((call) => call.id === item.call.id)
            ? {
                ...item,
                call: {
                  ...item.call,
                  status: 'completed',
                  result: quickstartChatReplyToolResult(item.call, trimmedReply, promptLocaleRef.current),
                  error: undefined,
                },
              }
            : item,
        ),
      );
    }
    const nextMessages: QuickstartMessage[] = [...conversationMessagesRef.current, replyMessage];
    conversationMessagesRef.current = nextMessages;
    await startQuickstartTurn(nextMessages);
  };

  const sendInitialPrompt = async () => {
    const trimmedPrompt = prompt.trim();
    if (!trimmedPrompt || creatingTemplateId) {
      return;
    }
    promptLocaleRef.current = locale;
    const config = createDialogAgentConfig(blankAgentTemplate, locale, undefined, modelMappings);
    setCreatingTemplateId(blankAgentTemplate.id);
    setChatError(null);
    setPrompt('');
    try {
      setCreatedTemplateId(blankAgentTemplate.id);
      setCreatedAgent(null);
      setCreatedAgentConfig(config);
      createdAgentRef.current = null;
      createdAgentConfigRef.current = config;
      setDetailTemplateId(null);
      setFormat('YAML');
      setAgentTab('config');
      setActiveStep(0);
      activeStepRef.current = 'agent';
      setSelectedEnvironment(null);
      selectedEnvironmentRef.current = null;
      setSelectedVaultIds([]);
      selectedVaultIdsRef.current = [];
      setSession(null);
      sessionRef.current = null;
      setTestRunStopped(false);
      setTestRunMessage('');
      setDeploymentSchedulePlanned(false);
      deploymentSchedulePlannedRef.current = false;
      initialAgentDescriptionRef.current = trimmedPrompt;
      conversationMessagesRef.current = [];
      setChatItems([{ id: quickstartItemId('user'), type: 'message', role: 'user', content: trimmedPrompt }]);
      await startQuickstartTurn();
    } finally {
      setCreatingTemplateId(null);
    }
  };

  const createAgentFromBuildConfig = async (call: QuickstartToolCall) => {
    const config = quickstartBuildAgentConfigInput(
      call.input,
      createdAgentConfigRef.current ??
        createDialogAgentConfig(blankAgentTemplate, promptLocaleRef.current, undefined, modelMappings),
      modelMappings,
    );
    setCreatedAgentConfig(config);
    createdAgentConfigRef.current = config;
    try {
      updateQuickstartTool(setChatItems, call.id, { status: 'running', error: undefined });
      const created = await createAgent(config, activeWorkspaceId);
      setCreatedTemplateId((current) => current ?? blankAgentTemplate.id);
      setCreatedAgent(created);
      setCreatedAgentConfig(config);
      createdAgentRef.current = created;
      createdAgentConfigRef.current = config;
      setDetailTemplateId(null);
      setFormat('YAML');
      setAgentTab('config');
      setChatItems((current) => {
        const toolIndex = current.findIndex((item) => item.type === 'tool' && item.call.id === call.id);
        if (toolIndex >= 0) {
          return current.map((item, index) =>
            index === toolIndex ? { id: item.id, type: 'create_agent_result' as const, agentConfig: config } : item,
          );
        }
        if (current.some((item) => item.type === 'create_agent_result')) {
          return current.map((item) => (item.type === 'create_agent_result' ? { ...item, agentConfig: config } : item));
        }
        return [
          ...current,
          { id: quickstartItemId('create_agent_result'), type: 'create_agent_result', agentConfig: config },
        ];
      });
      await completeAwaitingTool(call, { content: quickstartResultText().agentCreated });
    } catch (error) {
      await completeAwaitingTool(call, {
        content: quickstartResultText().createAgentFailed(errorMessage(error)),
        isError: true,
      });
    }
  };

  const startEnvironmentStep = async () => {
    if (isChatStreaming || !createdAgentConfigRef.current) {
      return;
    }
    const shouldStartEnvironmentTurn = activeStepRef.current === 'agent' && !selectedEnvironmentRef.current;
    setActiveStep(1);
    activeStepRef.current = 'environment';
    if (shouldStartEnvironmentTurn) {
      await startQuickstartTurn();
    }
  };

  const openPreviewEnvironmentStep = async () => {
    setAgentTab('preview');
    await startEnvironmentStep();
  };

  const completeOfferNextStep = async (call: QuickstartToolCall) => {
    const currentStep = activeStepRef.current;
    if (currentStep === 'agent') {
      setActiveStep(1);
      activeStepRef.current = 'environment';
    } else if (currentStep === 'environment') {
      setActiveStep(2);
      activeStepRef.current = 'session';
    } else if (currentStep === 'session') {
      setActiveStep(3);
      activeStepRef.current = 'integrate';
    }
    await completeAwaitingTool(call, { content: quickstartResultText().nextStepSelected });
  };

  const completeEnvironmentStep = async (call: QuickstartToolCall) => {
    setActiveStep(2);
    activeStepRef.current = 'session';
    await completeAwaitingTool(call, { content: call.result ?? quickstartResultText().environmentSelectedShort });
  };

  const completeVaultSelection = async (call: QuickstartToolCall) => {
    const t = quickstartResultText();
    const vaultIds = quickstartVaultIdsFromInput(call.input);
    if (vaultIds.length) {
      const nextVaultIds = Array.from(new Set([...selectedVaultIdsRef.current, ...vaultIds]));
      selectedVaultIdsRef.current = nextVaultIds;
      setSelectedVaultIds(nextVaultIds);
    }
    const resultText = vaultIds.length ? t.vaultSelected(vaultIds.join(', ')) : t.noVaultSelected;
    await completeAwaitingTool(call, { content: resultText });
  };

  const authorizeVaultCredentialFromTool = async (
    call: QuickstartToolCall,
    auth?: Record<string, unknown>,
    displayName?: string,
  ) => {
    const vaultId =
      typeof call.input.vault_id === 'string' && call.input.vault_id
        ? call.input.vault_id
        : selectedVaultIdsRef.current[0];
    const serverName =
      typeof call.input.mcp_server_name === 'string'
        ? call.input.mcp_server_name
        : quickstartResultText().thisMcpServer;
    const credentialAuth = auth ?? toRecord(call.input.auth);
    if (!vaultId) {
      updateQuickstartTool(setChatItems, call.id, {
        status: 'awaiting_user',
        error: quickstartResultText().selectVaultBeforeCredential,
      });
      return;
    }
    if (!credentialAuth) {
      updateQuickstartTool(setChatItems, call.id, {
        status: 'awaiting_user',
        error: quickstartResultText().credentialNotSupported(serverName),
      });
      return;
    }
    try {
      updateQuickstartTool(setChatItems, call.id, { status: 'running' });
      const credential = await createQuickstartVaultCredential(
        vaultId,
        { ...call.input, display_name: displayName, auth: credentialAuth },
        activeWorkspaceId,
      );
      await completeAwaitingTool(call, {
        content: quickstartResultText().credentialCreated(credential.id),
      });
    } catch (error) {
      await completeAwaitingTool(call, { content: errorMessage(error), isError: true });
    }
  };

  const skipVaultCredentialFromTool = async (call: QuickstartToolCall) => {
    const serverName = typeof call.input.mcp_server_name === 'string' ? titleCase(call.input.mcp_server_name) : 'MCP';
    await completeAwaitingTool(call, {
      content: quickstartResultText().credentialSkipped(serverName),
    });
  };

  const completeIntegrationExit = async (call: QuickstartToolCall, exit: 'scaffold' | 'go_to_agent') => {
    if (exit === 'go_to_agent') {
      const agentId =
        createdAgentRef.current?.id ||
        (typeof call.input.agent_id === 'string' && call.input.agent_id.trim() ? call.input.agent_id.trim() : '');
      await completeAwaitingTool(call, { content: quickstartResultText().exitedToAgentDetail });
      if (agentId) {
        window.location.assign(agentDetailHref(activeWorkspaceId, agentId));
      }
      return;
    }
    await completeAwaitingTool(call, { content: quickstartResultText().scaffoldCopied });
  };

  const createSessionFromTool = async (call: QuickstartToolCall) => {
    const agent = createdAgentRef.current;
    const environmentId = selectedEnvironmentRef.current?.id;
    if (!agent || !environmentId) {
      await completeAwaitingTool(call, {
        content: quickstartResultText().sessionRequiresAgentEnv,
        isError: true,
      });
      return;
    }
    try {
      updateQuickstartTool(setChatItems, call.id, { status: 'running' });
      const createdSession = await createQuickstartSession(
        agent,
        environmentId,
        selectedVaultIdsRef.current,
        activeWorkspaceId,
      );
      setSession(createdSession);
      sessionRef.current = createdSession;
      setTestRunStopped(false);
      setActiveStep(2);
      activeStepRef.current = 'session';
      setAgentTab('preview');
      const suggestedMessage =
        typeof call.input.suggested_first_message === 'string' ? call.input.suggested_first_message : '';
      setTestRunMessage(suggestedMessage);
      await completeAwaitingTool(call, {
        content: quickstartResultText().sessionCreatedWithMessage(createdSession.id, suggestedMessage),
      });
    } catch (error) {
      await completeAwaitingTool(call, { content: errorMessage(error), isError: true });
    }
  };

  const sendTestRunMessage = async (call: QuickstartToolCall, message: string) => {
    const trimmedMessage = message.trim();
    const activeSession = session;
    if (!trimmedMessage || !activeSession) {
      return;
    }
    try {
      updateQuickstartTool(setChatItems, call.id, { status: 'running' });
      await postQuickstartSessionMessage(activeSession.id, trimmedMessage, activeWorkspaceId);
      await completeAwaitingTool(call, { content: quickstartResultText().firstTestMessageSent(activeSession.id) });
    } catch (error) {
      await completeAwaitingTool(call, { content: errorMessage(error), isError: true });
    }
  };

  const activeAwaitTestRunCall = [...chatItems]
    .reverse()
    .find(
      (item): item is Extract<QuickstartChatItem, { type: 'tool' }> =>
        item.type === 'tool' && item.call.name === 'await_test_run' && item.call.status === 'awaiting_user',
    )?.call;
  const activeOfferNextStepCall = [...chatItems]
    .reverse()
    .find(
      (item): item is Extract<QuickstartChatItem, { type: 'tool' }> =>
        item.type === 'tool' && item.call.name === 'offer_next_step' && item.call.status === 'awaiting_user',
    )?.call;
  const hasAwaitingOfferNextStep = chatItems.some(
    (item) => item.type === 'tool' && item.call.name === 'offer_next_step' && item.call.status === 'awaiting_user',
  );
  const isReplyBlocked = hasAwaitingQuickstartQuestionSet(chatItems);

  const sendPreviewTestRunMessage = async () => {
    if (isChatStreaming) {
      return;
    }
    const message = testRunMessage;
    if (!message.trim()) {
      return;
    }
    setTestRunMessage('');
    if (activeAwaitTestRunCall) {
      await sendTestRunMessage(activeAwaitTestRunCall, message);
      return;
    }
    const activeSession = sessionRef.current;
    if (!activeSession) {
      return;
    }
    try {
      await postQuickstartSessionMessage(activeSession.id, message, activeWorkspaceId);
    } catch (error) {
      const errorText = errorMessage(error);
      setChatError(errorText);
    }
  };

  const startHeaderTestRun = async () => {
    setAgentTab('preview');
    const agent = createdAgentRef.current;
    const environmentId = selectedEnvironmentRef.current?.id;
    if (!agent || !environmentId) {
      return;
    }
    try {
      const createdSession = await createQuickstartSession(
        agent,
        environmentId,
        selectedVaultIdsRef.current,
        activeWorkspaceId,
      );
      setSession(createdSession);
      sessionRef.current = createdSession;
      setTestRunStopped(false);
      setActiveStep(2);
      activeStepRef.current = 'session';
      if (activeOfferNextStepCall) {
        await completeAwaitingTool(activeOfferNextStepCall, {
          content: quickstartResultText().sessionCreated(createdSession.id),
        });
      }
    } catch (error) {
      const message = errorMessage(error);
      setChatError(message);
    }
  };

  const stopTestRun = async () => {
    setAgentTab('preview');
    const activeSession = sessionRef.current;
    if (!activeSession) {
      return;
    }
    setTestRunStopped(true);
    setTestRunMessage('');
    try {
      await interruptQuickstartSession(activeSession.id, activeWorkspaceId);
    } catch (error) {
      const message = errorMessage(error);
      setChatError(message);
    }
    if (activeAwaitTestRunCall) {
      await completeAwaitingTool(activeAwaitTestRunCall, {
        content: quickstartResultText().sessionClosed(activeSession.id),
      });
    }
  };

  return (
    <section className="relative flex h-[calc(100vh-48px)] min-h-[650px] flex-col text-foreground">
      <QuickstartHeader
        activeStep={activeStep}
        canTestRun={Boolean(selectedEnvironment)}
        hasAgent={Boolean(createdAgent)}
        hasTemplate={Boolean(createdTemplate)}
        isTestRunActive={Boolean(session && !testRunStopped)}
        onTitleClick={createdTemplate ? resetQuickstart : () => searchRef.current?.focus()}
        onToggleTestRun={() => {
          if (session && !testRunStopped) {
            void stopTestRun();
            return;
          }
          void startHeaderTestRun();
        }}
      />

      <div
        ref={quickstartGridRef}
        data-testid="quickstart-layout"
        data-inspector-width={Math.round(quickstartInspectorPaneWidth)}
        className="min-h-0 flex-1 pt-6"
      >
        <ResizablePanelGroup
          id="quickstart-resizable-panels"
          orientation="horizontal"
          className="min-h-0"
          onLayoutChange={(layout) => {
            const inspectorSize = layout['quickstart-inspector'];
            if (typeof inspectorSize === 'number' && quickstartGridWidth > 0) {
              setQuickstartInspectorPaneWidth(Math.round((quickstartGridWidth * inspectorSize) / 100));
            }
          }}
        >
          <ResizablePanel id="quickstart-primary" minSize={quickstartPrimaryPaneMinWidth} className="min-w-0">
            {createdTemplate ? (
              <QuickstartChatPane
                agent={createdAgent}
                agentConfig={createdAgentConfig}
                environment={selectedEnvironment}
                session={session}
                chatItems={chatItems}
                interactionResultText={quickstartResultText()}
                isStreaming={isChatStreaming}
                isReplyBlocked={isReplyBlocked}
                error={chatError}
                reply={reply}
                onReplyChange={setReply}
                onSubmitReply={sendChatReply}
                onCompleteTool={completeAwaitingTool}
                onCompleteEnvironmentTool={completeEnvironmentStep}
                onConfirmVaultSelection={completeVaultSelection}
                onOfferNextStep={completeOfferNextStep}
                onCreateAgentFromConfig={createAgentFromBuildConfig}
                onAuthorizeCredential={authorizeVaultCredentialFromTool}
                onSkipCredential={skipVaultCredentialFromTool}
                onCreateSession={createSessionFromTool}
                onSendTestRunMessage={sendTestRunMessage}
                onIntegrationExit={completeIntegrationExit}
                onStartEnvironmentStep={startEnvironmentStep}
                offerNextStepLabel={
                  activeStep === 0
                    ? msg('managedAgents.quickstart.next.configureEnvironment', 'Next: Configure environment')
                    : activeStep === 1
                      ? msg('managedAgents.quickstart.next.startSession', 'Next: Start session')
                      : undefined
                }
                showCreateAgentNext={activeStep === 0 && !hasAwaitingOfferNextStep}
              />
            ) : (
              <InitialPromptPane
                prompt={prompt}
                isCreating={Boolean(creatingTemplateId)}
                onPromptChange={setPrompt}
                onSubmit={sendInitialPrompt}
              />
            )}
          </ResizablePanel>

          <ResizableHandle
            aria-label={msg('managedAgents.quickstart.resizePanels', 'Resize quickstart panels')}
            withHandle
            className="cursor-col-resize bg-transparent focus-visible:[&>div]:ring-2 focus-visible:[&>div]:ring-ring/50"
            handleClassName="h-6 w-4 rounded-md border-border/70 bg-background/95 text-muted-foreground/70 shadow-none transition-[background-color,border-color,color] hover:border-border hover:bg-accent hover:text-accent-foreground [&_svg]:size-2.5"
          />

          <ResizablePanel
            id="quickstart-inspector"
            panelRef={quickstartInspectorPanelRef}
            minSize={quickstartInspectorPaneMinWidth}
            maxSize={quickstartInspectorPaneMaxWidth}
            defaultSize={quickstartInspectorPaneDefaultWidth}
            groupResizeBehavior="preserve-pixel-size"
            className="min-w-0"
            onResize={(size) => {
              const nextWidth =
                size.inPixels > 0
                  ? size.inPixels
                  : quickstartGridWidth > 0
                    ? (quickstartGridWidth * size.asPercentage) / 100
                    : 0;
              if (nextWidth > 0) {
                setQuickstartInspectorPaneWidth(Math.round(nextWidth));
              }
            }}
          >
            {createdTemplate ? (
              <CreatedAgentConfigPanel
                template={createdTemplate}
                agent={createdAgent}
                agentConfig={createdAgentConfig}
                environment={selectedEnvironment}
                session={session}
                testRunStopped={testRunStopped}
                workspaceId={activeWorkspaceId}
                testRunMessage={testRunMessage}
                format={format}
                tab={agentTab}
                canSendTestRunMessage={
                  Boolean(session && (activeAwaitTestRunCall || session.status === 'idle')) &&
                  !isChatStreaming &&
                  !testRunStopped
                }
                onTestRunMessageChange={setTestRunMessage}
                onSendTestRunMessage={sendPreviewTestRunMessage}
                onRerunTestRun={startHeaderTestRun}
                onConfigureEnvironment={openPreviewEnvironmentStep}
                onFormatChange={setFormat}
                onTabChange={setAgentTab}
                modelMappings={modelMappings}
              />
            ) : detailTemplate ? (
              <TemplateDetailPanel
                template={detailTemplate}
                format={format}
                onBack={() => setDetailTemplateId(null)}
                onFormatChange={setFormat}
                onUseTemplate={() => handleUseTemplate(detailTemplate)}
                isUsing={creatingTemplateId === detailTemplate.id}
                modelMappings={modelMappings}
              />
            ) : (
              <BrowseTemplatesPanel
                query={query}
                searchRef={searchRef}
                visibleTemplates={visibleTemplates}
                onQueryChange={setQuery}
                onTemplateClick={handleTemplateClick}
              />
            )}
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>
    </section>
  );
}
