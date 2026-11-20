import { gitResourceBody, resourceFormValues } from '../resources/git-resource';
import { sessionFileResourcePayload } from '../sessions/file-resource-form';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import { ManagedDetailBreadcrumb } from '../components/breadcrumbs';
import { channelApi, channelVaultIds, channelResourceValues, type Channel, type ChannelQr } from './api';
import {
  StepConnect,
  StepMa,
  StepStrategy,
  WizardSideCards,
  WizardStepper,
  type WizardSideResource,
  type WizardStep,
  type WizardValue,
} from './ChannelWizardForm';
import { channelTargetsReady, channelWizardStepReady, wizardStepAllowed } from './channelInput';
import { useChannelResources } from './channelResources';
import { useChannelAuthorization } from './useChannelAuthorization';
import { listChannelEnvironments } from './channelEnvironments';
import { listAllWorkspaceAgents } from './workspaceChannels';
import { channelDetailPath, channelsListPath } from './paths';
import type { AgentApiResponse, EnvironmentApiResponse } from '../types';
import { errorMessage } from '../utils';

function WizardStepFields({
  step,
  value,
  onChange,
  binding,
  onBindingChange,
  saved,
  qr,
  busy,
  workspaceId,
  agents,
  environments,
  agentLocked,
  onGenerateQr,
  showQr,
}: {
  step: WizardStep;
  value: WizardValue;
  onChange: (value: WizardValue) => void;
  binding: string;
  onBindingChange: (binding: string) => void;
  saved: Channel | null;
  qr: ChannelQr | null;
  busy: boolean;
  workspaceId: string;
  agents: AgentApiResponse[];
  environments: EnvironmentApiResponse[];
  agentLocked: boolean;
  onGenerateQr: (openAuthorization?: boolean) => void;
  showQr: boolean;
}) {
  if (step === 2)
    return <StepStrategy workspaceId={workspaceId} value={value} onChange={onChange} modeLocked={Boolean(saved)} />;
  if (step === 3) {
    return (
      <StepMa
        workspaceId={workspaceId}
        value={value}
        onChange={onChange}
        agents={agents}
        environments={environments}
        agentLocked={agentLocked}
      />
    );
  }
  return (
    <StepConnect
      value={value}
      onChange={onChange}
      binding={binding}
      onBindingChange={onBindingChange}
      saved={saved}
      qr={qr}
      busy={busy}
      workspaceId={workspaceId}
      onGenerateQr={onGenerateQr}
      showQr={showQr}
    />
  );
}

export function ChannelWizardFormPage({
  mode,
  workspaceId,
  initialAgentId,
  initialChannel,
  initialValue,
  onNavigate,
  title,
}: {
  mode: 'create' | 'edit';
  workspaceId: string;
  initialAgentId: string;
  initialChannel: Channel | null;
  initialValue: WizardValue;
  onNavigate: (href: string) => void;
  title: string;
}) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const [step, setStep] = useState<WizardStep>(1);
  const [binding, setBinding] = useState(initialChannel?.binding_status === 'bound' ? 'credentials' : 'qr');
  const [saved, setSaved] = useState<Channel | null>(initialChannel);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [pollError, setPollError] = useState('');
  const [value, setValue] = useState<WizardValue>(initialValue);
  const onAuthorized = useCallback((appId: string) => {
    setValue((current) => ({ ...current, app_id: appId, app_secret: '' }));
  }, []);
  const {
    authorization,
    qr,
    setQr,
    showQr,
    authorizing,
    authorize,
    pollError: authorizationPollError,
  } = useChannelAuthorization(workspaceId, binding === 'qr', onAuthorized, setError);
  const busy = saving || authorizing;

  const agentsQuery = useQuery({
    queryKey: ['workspace-channel-agents', workspaceId],
    queryFn: () => listAllWorkspaceAgents(workspaceId),
  });
  const environmentsQuery = useQuery({
    queryKey: ['channel-environment-options', workspaceId],
    queryFn: () => listChannelEnvironments(workspaceId),
  });

  const targetAgentId = value.agentId || initialAgentId;
  const authorizationId = authorization?.id;
  const qrStatus = qr?.status;

  useEffect(() => {
    if (
      binding !== 'qr' ||
      authorizationId ||
      !saved ||
      saved.binding_status === 'bound' ||
      !targetAgentId ||
      (qrStatus && qrStatus !== 'pending')
    )
      return;
    const controller = new AbortController();
    let inFlight = false;
    const poll = async () => {
      if (inFlight) return;
      inFlight = true;
      try {
        const page = await channelApi<{ data: Channel[] }>(targetAgentId, workspaceId);
        if (controller.signal.aborted) return;
        const channel = page.data.find((item) => item.id === saved.id);
        if (channel?.binding_status === 'bound') {
          setSaved(channel);
          setValue((current) => ({ ...current, app_id: channel.app_id ?? '', app_secret: '' }));
          setQr((current) => (current ? { ...current, status: 'bound' } : null));
          setPollError('');
          void queryClient.invalidateQueries({ queryKey: ['workspace-channels', workspaceId] });
          void queryClient.invalidateQueries({ queryKey: ['channel-detail', workspaceId, targetAgentId, saved.id] });
          return;
        }
        const next = await anthropicBetaApi.channels.qr<ChannelQr>(workspaceId, saved.id, targetAgentId);
        if (!controller.signal.aborted) {
          setQr(next);
          setPollError('');
        }
      } catch (err) {
        if (!controller.signal.aborted) setPollError(errorMessage(err));
      } finally {
        inFlight = false;
      }
    };
    void poll();
    const timer = window.setInterval(() => void poll(), 2500);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [saved, targetAgentId, workspaceId, queryClient, authorizationId, binding, qrStatus, setQr]);

  const agents = useMemo(() => agentsQuery.data ?? [], [agentsQuery.data]);
  const environments = useMemo(() => environmentsQuery.data ?? [], [environmentsQuery.data]);
  const channelResources = useChannelResources(workspaceId);
  const agentLocked = Boolean(saved);

  const access = useMemo(() => ({ binding, saved, qrStatus: qr?.status ?? null }), [binding, saved, qr?.status]);
  const canGoNext = useMemo(() => channelWizardStepReady(step, value, access), [step, value, access]);
  const openStep = (next: WizardStep) => {
    if (wizardStepAllowed(next, value, access)) setStep(next);
  };

  const canSave =
    (mode === 'edit' || step === 3) &&
    channelWizardStepReady(1, value, access) &&
    channelWizardStepReady(3, value, access);
  const persist = async () => {
    if (!channelTargetsReady(value)) {
      setError(connectTargetError(value, msg));
      return;
    }
    if (!canSave) return;
    setSaving(true);
    setError('');
    try {
      const next = await channelApi<Channel>(targetAgentId!, workspaceId, saved ? `/${saved.id}` : '', 'POST', {
        ...channelPayload(value, binding),
        environment_id: value.environmentId,
        ...(binding === 'qr' && authorization ? { authorization_id: authorization.id } : {}),
      });
      setSaved(next);
      queryClient.setQueryData(['channel-wizard', workspaceId, targetAgentId, next.id], {
        channel: next,
        environmentId: value.environmentId,
      });
      setValue((current) => ({
        ...current,
        app_secret: '',
        agentId: targetAgentId!,
        ...resourceFormValues(channelResourceValues(next)),
      }));
      void queryClient.invalidateQueries({ queryKey: ['workspace-channels', workspaceId] });
      void queryClient.invalidateQueries({ queryKey: ['channel-detail', workspaceId, targetAgentId, next.id] });
      onNavigate(channelDetailPath(workspaceId, targetAgentId!, next.id));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const targetNames = useMemo(() => {
    const resources: WizardSideResource[] = [];
    const environment = environments.find((item) => item.id === value.environmentId);
    if (environment) resources.push({ kind: 'environment', label: environment.name });
    const memoryName = channelResources.data?.memoryStores.find((item) => item.id === value.memory_store_id)?.label;
    if (value.memory_store_id) resources.push({ kind: 'memory', label: memoryName ?? value.memory_store_id });
    for (const vaultId of channelVaultIds(value)) {
      const vaultName = channelResources.data?.vaults.find((item) => item.id === vaultId)?.label;
      resources.push({ kind: 'vault', label: vaultName ?? vaultId });
    }
    for (const file of value.fileResources) resources.push({ kind: 'file', label: file.fileId });
    for (const git of value.gitResources) if (git.url) resources.push({ kind: 'git', label: git.url });
    return {
      agentName: agents.find((agent) => agent.id === value.agentId)?.name ?? '',
      resources,
    };
  }, [
    agents,
    environments,
    channelResources.data,
    value.agentId,
    value.environmentId,
    value.memory_store_id,
    value.vault_id,
    value.vault_ids,
    value.gitResources,
    value.fileResources,
  ]);
  const visibleError = [
    error,
    pollError,
    authorizationPollError,
    ...[agentsQuery.error, environmentsQuery.error].filter(Boolean).map(errorMessage),
  ].find(Boolean);
  const listHref = channelsListPath(workspaceId);
  return (
    <section className="flex min-h-[calc(100dvh-112px)] min-w-0 flex-col text-foreground md:min-h-[calc(100dvh-48px)]">
      <ManagedDetailBreadcrumb
        listHref={listHref}
        listLabel={msg('managedAgents.channels.title', '渠道')}
        currentLabel={title}
        className="-mt-1 mb-6 shrink-0"
      />
      <div className="grid min-w-0 flex-1 items-start gap-6 xl:grid-cols-[minmax(0,1fr)_280px]">
        <div className="mx-auto flex w-full max-w-[880px] min-w-0 flex-col">
          <header className="mb-7 shrink-0">
            <h1 className="text-2xl font-medium tracking-tight">{title}</h1>
          </header>
          <WizardStepFields
            step={step}
            value={value}
            onChange={setValue}
            binding={binding}
            onBindingChange={(next) => {
              setBinding(next);
              setPollError('');
            }}
            saved={saved}
            qr={qr}
            busy={busy}
            workspaceId={workspaceId}
            agents={agents}
            environments={environments}
            agentLocked={agentLocked}
            onGenerateQr={(openAuthorization) => {
              setPollError('');
              void authorize(value.name, Boolean(openAuthorization));
            }}
            showQr={showQr}
          />
          {visibleError ? (
            <Alert variant="destructive" className="mb-3">
              <AlertDescription>{visibleError}</AlertDescription>
            </Alert>
          ) : null}
          <ChannelWizardFooter
            mode={mode}
            step={step}
            busy={busy}
            canGoNext={canGoNext}
            canSave={canSave}
            listHref={listHref}
            onNavigate={onNavigate}
            onNextStep={() => openStep((step + 1) as WizardStep)}
            onPersist={() => void persist()}
          />
        </div>
        <aside
          className="min-w-0 space-y-5 xl:sticky xl:top-0"
          aria-label={msg('managedAgents.channels.progress', '进度')}
        >
          <WizardStepper
            step={step}
            canOpen={(next) => wizardStepAllowed(next, value, access)}
            onStepChange={openStep}
          />
          <WizardSideCards {...targetNames} />
        </aside>
      </div>
    </section>
  );
}

function ChannelWizardFooter({
  mode,
  step,
  busy,
  canGoNext,
  canSave,
  listHref,
  onNavigate,
  onNextStep,
  onPersist,
}: {
  mode: 'create' | 'edit';
  step: WizardStep;
  busy: boolean;
  canGoNext: boolean;
  canSave: boolean;
  listHref: string;
  onNavigate: (href: string) => void;
  onNextStep: () => void;
  onPersist: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="sticky bottom-0 z-10 flex shrink-0 items-center justify-end gap-2 border-t-[0.5px] border-border bg-background/95 py-3 backdrop-blur-xs">
      <Button type="button" variant="outline" disabled={busy} onClick={() => onNavigate(listHref)}>
        {msg('managedAgents.channels.cancel', '取消')}
      </Button>
      {mode === 'edit' && step < 3 ? (
        <Button type="button" variant="secondary" disabled={busy || !canSave} onClick={onPersist}>
          {busy
            ? msg('managedAgents.channels.processing', '处理中…')
            : msg('managedAgents.channels.saveConfig', '保存配置')}
        </Button>
      ) : null}
      {step < 3 ? (
        <Button type="button" disabled={!canGoNext || busy} onClick={onNextStep}>
          {msg('managedAgents.channels.nextStep', '下一步')}
        </Button>
      ) : (
        <Button type="button" disabled={busy || !canSave} onClick={onPersist}>
          {busy
            ? msg('managedAgents.channels.processing', '处理中…')
            : msg('managedAgents.channels.saveConfig', '保存配置')}
        </Button>
      )}
    </div>
  );
}

function connectTargetError(value: WizardValue, msg: ReturnType<typeof useI18n>['msg']) {
  if (!value.name.trim()) return msg('managedAgents.channels.nameRequired', '请输入机器人名称');
  if (!value.agentId) return msg('managedAgents.channels.agentRequired', '请选择智能体');
  return msg('managedAgents.channels.environmentRequired', '请先选择执行环境，再保存渠道');
}

function channelPayload(value: WizardValue, binding: string) {
  const appId = value.app_id?.trim() ?? '';
  const appSecret = value.app_secret?.trim() ?? '';
  return {
    name: value.name.trim(),
    mode: value.mode,
    pairing_oauth_provider: value.mode === 'pairing' ? (value.pairing_oauth_provider ?? '') : '',
    ...(value.mode === 'pairing' && value.pairing_oauth_provider ? { identity_resolution: { mode: 'pairing' } } : {}),
    agent_version: value.agent_version ?? null,
    memory_store_id: value.memory_store_id ?? null,
    vault_ids: channelVaultIds(value),
    resources: value.resourcesChanged
      ? [...value.fileResources.map(sessionFileResourcePayload), ...value.gitResources.map(gitResourceBody)]
      : value.originalResources.map(({ tokenConfigured: _configured, ...resource }) => resource),
    include_thinking: value.include_thinking,
    include_tool_calls: value.include_tool_calls,
    reply_in_thread: value.reply_in_thread,
    ...(binding === 'credentials' && appSecret ? { app_id: appId, app_secret: appSecret } : {}),
  };
}
