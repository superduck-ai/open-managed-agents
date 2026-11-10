import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Button } from '../../../shared/ui/button';
import { ManagedDetailBreadcrumb } from '../components/breadcrumbs';
import { channelApi, type Channel, type ChannelQr } from './api';
import {
  StepConnect,
  StepMa,
  StepStrategy,
  WizardSideCards,
  WizardStepper,
  type WizardStep,
  type WizardValue,
} from './ChannelWizardForm';
import { channelTargetsReady, channelWizardStepReady, wizardStepAllowed } from './channelInput';
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
  if (step === 2) return <StepStrategy value={value} onChange={onChange} />;
  if (step === 3) {
    return (
      <StepMa value={value} onChange={onChange} agents={agents} environments={environments} agentLocked={agentLocked} />
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
  const [value, setValue] = useState<WizardValue>(initialValue);
  const savedEnvironment = useRef(
    initialChannel ? { agentId: initialValue.agentId, environmentId: initialValue.environmentId } : null,
  );
  const onAuthorized = useCallback((appId: string) => {
    setValue((current) => ({ ...current, app_id: appId }));
    setError('');
  }, []);
  const { authorization, qr, setQr, showQr, authorizing, authorize } = useChannelAuthorization(
    workspaceId,
    onAuthorized,
    setError,
  );
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

  useEffect(() => {
    if (authorization || !saved || saved.binding_status === 'bound' || !targetAgentId) return;
    const controller = new AbortController();
    const poll = async () => {
      try {
        const page = await channelApi<{ data: Channel[] }>(targetAgentId, workspaceId);
        if (controller.signal.aborted) return;
        const channel = page.data.find((item) => item.id === saved.id);
        if (channel?.binding_status === 'bound') {
          setSaved(channel);
          setValue((current) => ({ ...current, app_id: channel.app_id ?? '' }));
          setQr((current) => (current ? { ...current, status: 'bound' } : null));
          setError('');
          void queryClient.invalidateQueries({ queryKey: ['workspace-channels', workspaceId] });
          void queryClient.invalidateQueries({ queryKey: ['channel-detail', workspaceId, targetAgentId, saved.id] });
          return;
        }
        const next = await channelApi<ChannelQr>(targetAgentId, workspaceId, `/${saved.id}/qr`);
        if (!controller.signal.aborted) setQr(next);
      } catch (err) {
        if (!controller.signal.aborted) setError(errorMessage(err));
      }
    };
    void poll();
    const timer = window.setInterval(() => void poll(), 2500);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [saved, targetAgentId, workspaceId, queryClient, authorization?.id]);

  const agents = agentsQuery.data ?? [];
  const environments = environmentsQuery.data ?? [];
  const agentLocked = Boolean(saved);

  const access = useMemo(() => ({ binding, saved, qrStatus: qr?.status ?? null }), [binding, saved, qr?.status]);
  const canGoNext = useMemo(() => channelWizardStepReady(step, value, access), [step, value, access]);
  const openStep = (next: WizardStep) => {
    if (wizardStepAllowed(next, value, access)) setStep(next);
  };

  const canSave =
    step === 3 && channelWizardStepReady(1, value, access) && Boolean(value.agentId && value.environmentId);
  const persist = async () => {
    if (!channelTargetsReady(value)) {
      setError(connectTargetError(value, msg));
      return;
    }
    if (!canSave) return;
    setSaving(true);
    setError('');
    try {
      if (
        savedEnvironment.current?.agentId !== targetAgentId ||
        savedEnvironment.current?.environmentId !== value.environmentId
      ) {
        await channelApi(targetAgentId!, workspaceId, '/settings', 'POST', { environment_id: value.environmentId });
        savedEnvironment.current = { agentId: targetAgentId, environmentId: value.environmentId };
      }
      const next = await channelApi<Channel>(targetAgentId!, workspaceId, saved ? `/${saved.id}` : '', 'POST', {
        ...channelPayload(value, binding),
        ...(binding === 'qr' && authorization ? { authorization_id: authorization.id } : {}),
      });
      setSaved(next);
      setValue((current) => ({ ...current, app_secret: '', agentId: targetAgentId! }));
      void queryClient.invalidateQueries({ queryKey: ['workspace-channels', workspaceId] });
      void queryClient.invalidateQueries({ queryKey: ['channel-detail', workspaceId, targetAgentId, next.id] });
      onNavigate(channelDetailPath(workspaceId, targetAgentId!, next.id));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  const targetNames = useMemo(
    () => ({
      agentName: agents.find((agent) => agent.id === value.agentId)?.name ?? '',
      environmentName: environments.find((environment) => environment.id === value.environmentId)?.name ?? '',
    }),
    [agents, environments, value.agentId, value.environmentId],
  );
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
            onBindingChange={setBinding}
            saved={saved}
            qr={qr}
            busy={busy}
            workspaceId={workspaceId}
            agents={agents}
            environments={environments}
            agentLocked={agentLocked}
            onGenerateQr={(openAuthorization) => void authorize(value.name, Boolean(openAuthorization))}
            showQr={showQr}
          />
          {error ? (
            <Alert variant="destructive" className="mb-3">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
          <div className="sticky bottom-0 z-10 flex shrink-0 items-center justify-between gap-3 border-t-[0.5px] border-border bg-background py-3">
            <div>
              {step < 3 ? (
                <Button
                  type="button"
                  variant="secondary"
                  disabled={!canGoNext || busy}
                  onClick={() => openStep((step + 1) as WizardStep)}
                >
                  {msg('managedAgents.channels.nextStep', '下一步')}
                </Button>
              ) : null}
            </div>
            <div className="flex gap-2">
              <Button type="button" variant="outline" disabled={busy} onClick={() => onNavigate(listHref)}>
                {msg('managedAgents.channels.cancel', '取消')}
              </Button>
              <Button type="button" disabled={busy || !canSave} onClick={() => void persist()}>
                {busy
                  ? msg('managedAgents.channels.processing', '处理中…')
                  : msg('managedAgents.channels.saveConfig', '保存配置')}
              </Button>
            </div>
          </div>
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
    include_thinking: value.include_thinking,
    include_tool_calls: value.include_tool_calls,
    reply_in_thread: value.reply_in_thread,
    ...(binding === 'credentials' && appSecret ? { app_id: appId, app_secret: appSecret } : {}),
  };
}
