import { useQuery } from '@tanstack/react-query';
import { ArrowLeft } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { Button } from '../../../shared/ui/button';
import { channelApi, type Channel, type ChannelQr } from './api';
import {
  StepConnect,
  StepMa,
  StepStrategy,
  WizardStepper,
  type WizardStep,
  type WizardValue,
} from './ChannelWizardForm';
import { validateChannelInput } from './channelInput';
import { listAllWorkspaceAgents } from './workspaceChannels';
import { channelDetailPath, channelsListPath } from './paths';
import type { EnvironmentApiResponse } from '../types';
import { errorMessage } from '../utils';

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
  const [step, setStep] = useState<WizardStep>(1);
  const [binding, setBinding] = useState(initialChannel?.binding_status === 'bound' ? 'credentials' : 'qr');
  const [saved, setSaved] = useState<Channel | null>(initialChannel);
  const [qr, setQr] = useState<ChannelQr | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [value, setValue] = useState<WizardValue>(initialValue);

  const agentsQuery = useQuery({
    queryKey: ['workspace-channel-agents', workspaceId],
    queryFn: () => listAllWorkspaceAgents(workspaceId),
  });
  const environmentsQuery = useQuery({
    queryKey: ['channel-environment-options', workspaceId],
    queryFn: () => listEnvironments(workspaceId),
  });

  const targetAgentId = value.agentId || initialAgentId;

  useEffect(() => {
    if (!saved || qr?.status !== 'pending' || !targetAgentId) return;
    const controller = new AbortController();
    const timer = window.setInterval(() => {
      void channelApi<ChannelQr>(targetAgentId, workspaceId, `/${saved.id}/qr`)
        .then((next) => {
          if (controller.signal.aborted) return;
          setQr(next);
          if (next.status === 'bound') setSaved({ ...saved, binding_status: 'bound' });
        })
        .catch((err) => {
          if (!controller.signal.aborted) {
            setError(errorMessage(err));
            setQr(null);
          }
        });
    }, 2500);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [saved, qr?.status, targetAgentId, workspaceId]);

  const agents = agentsQuery.data ?? [];
  const environments = environmentsQuery.data ?? [];
  const agentLocked = mode === 'edit' || Boolean(initialAgentId);

  const canGoNext = useMemo(() => {
    if (step === 1) return Boolean(value.name.trim());
    if (step === 2) return true;
    return Boolean(value.agentId && value.environmentId);
  }, [step, value]);

  const persist = async (generateQr: boolean, closeAfter: boolean) => {
    const blocked = validateWizardBeforeSave({
      targetAgentId,
      value,
      binding,
      saved,
      msg,
      setError,
      setStep,
    });
    if (blocked) return;
    setBusy(true);
    setError('');
    try {
      const next = await saveWizardChannel({
        targetAgentId: targetAgentId!,
        workspaceId,
        saved,
        value,
        binding,
      });
      setSaved(next);
      setValue((current) => ({ ...current, app_secret: '', agentId: targetAgentId! }));
      if (generateQr) {
        setQr(await channelApi<ChannelQr>(targetAgentId!, workspaceId, `/${next.id}/qr`, 'POST', {}));
        setStep(1);
        return;
      }
      if (closeAfter) onNavigate(channelDetailPath(workspaceId, targetAgentId!, next.id));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="min-w-0 text-foreground">
      <button
        type="button"
        className="mb-4 inline-flex items-center gap-2 text-sm text-muted-foreground hover:text-foreground"
        onClick={() => onNavigate(channelsListPath(workspaceId))}
      >
        <ArrowLeft className="size-4" />
        {title}
      </button>
      <div className="rounded-xl border border-border bg-card p-6">
        <div className="grid items-start gap-6 md:grid-cols-[220px_minmax(0,1fr)]">
          <WizardStepper step={step} onStepChange={setStep} />
          <div>
            {step === 1 ? (
              <StepConnect
                value={value}
                onChange={setValue}
                binding={binding}
                onBindingChange={setBinding}
                saved={saved}
                qr={qr}
                busy={busy}
                onGenerateQr={() => void persist(true, false)}
              />
            ) : null}
            {step === 2 ? <StepStrategy value={value} onChange={setValue} modeLocked={Boolean(saved)} /> : null}
            {step === 3 ? (
              <StepMa
                value={value}
                onChange={setValue}
                agents={agents}
                environments={environments}
                agentLocked={agentLocked}
              />
            ) : null}
            {error ? (
              <p role="alert" className="mt-4 text-sm text-destructive">
                {error}
              </p>
            ) : null}
            <div className="mt-6 flex items-center justify-between gap-3">
              <div>
                {step < 3 ? (
                  <Button
                    type="button"
                    variant="secondary"
                    disabled={!canGoNext || busy}
                    onClick={() => setStep((step + 1) as WizardStep)}
                  >
                    {msg('managedAgents.channels.nextStep', '下一步')}
                  </Button>
                ) : null}
              </div>
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy}
                  onClick={() => onNavigate(channelsListPath(workspaceId))}
                >
                  {msg('managedAgents.channels.cancel', '取消')}
                </Button>
                <Button type="button" disabled={busy} onClick={() => void persist(false, true)}>
                  {busy
                    ? msg('managedAgents.channels.processing', '处理中…')
                    : msg('managedAgents.channels.saveConfig', '保存配置')}
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

async function listEnvironments(workspaceId: string) {
  const result: EnvironmentApiResponse[] = [];
  let cursor: string | null | undefined;
  do {
    const page = await anthropicBetaApi.environments.list<EnvironmentApiResponse>(
      { limit: 100, include_archived: false, ...(cursor ? { page: cursor } : {}) },
      workspaceId,
    );
    result.push(...page.data);
    cursor = page.next_page;
  } while (cursor);
  return result;
}

function validateWizardBeforeSave({
  targetAgentId,
  value,
  binding,
  saved,
  msg,
  setError,
  setStep,
}: {
  targetAgentId: string;
  value: WizardValue;
  binding: string;
  saved: Channel | null;
  msg: ReturnType<typeof useI18n>['msg'];
  setError: (error: string) => void;
  setStep: (step: WizardStep) => void;
}) {
  if (!targetAgentId) {
    setError(msg('managedAgents.channels.agentRequired', '请选择智能体'));
    setStep(3);
    return true;
  }
  if (!value.environmentId) {
    setError(msg('managedAgents.channels.environmentRequired', '请先选择执行环境，再保存渠道'));
    setStep(3);
    return true;
  }
  const validationError = validateChannelInput(value, binding, saved, msg);
  if (validationError) {
    setError(validationError);
    setStep(1);
    return true;
  }
  return false;
}

async function saveWizardChannel({
  targetAgentId,
  workspaceId,
  saved,
  value,
  binding,
}: {
  targetAgentId: string;
  workspaceId: string;
  saved: Channel | null;
  value: WizardValue;
  binding: string;
}) {
  const next = await channelApi<Channel>(
    targetAgentId,
    workspaceId,
    saved ? `/${saved.id}` : '',
    'POST',
    channelPayload(value, binding),
  );
  await channelApi(targetAgentId, workspaceId, '/settings', 'POST', { environment_id: value.environmentId });
  return next;
}

function channelPayload(value: WizardValue, binding: string) {
  return {
    name: value.name,
    mode: value.mode,
    include_thinking: value.include_thinking,
    include_tool_calls: value.include_tool_calls,
    reply_in_thread: value.reply_in_thread,
    ...(binding === 'credentials' && value.app_secret ? { app_id: value.app_id, app_secret: value.app_secret } : {}),
  };
}
