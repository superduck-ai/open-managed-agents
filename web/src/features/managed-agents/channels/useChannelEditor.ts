import { useI18n } from '../../../shared/i18n';
import { useEffect, useState } from 'react';
import { channelApi, type Channel, type ChannelInput, type ChannelQr } from './api';
import { useChannelEnvironment } from './useChannelEnvironment';
import { errorMessage } from '../utils';

export interface ChannelEditorProps {
  agentId: string;
  workspaceId: string;
  channel: Channel | null;
  onClose: () => void;
  onSaved: () => void;
  finalFocus: () => HTMLElement | null;
}
export function useChannelEditor({ agentId, workspaceId, channel, onClose, onSaved }: ChannelEditorProps) {
  const { msg } = useI18n();
  const environment = useChannelEnvironment(agentId, workspaceId);
  const [value, setValue] = useState<ChannelInput>({
    name: channel?.name ?? '',
    mode: channel?.mode ?? 'open',
    include_thinking: channel?.include_thinking ?? false,
    include_tool_calls: channel?.include_tool_calls ?? false,
    reply_in_thread: channel?.reply_in_thread ?? false,
    app_id: channel?.app_id ?? '',
  });
  const [binding, setBinding] = useState(channel?.binding_status === 'bound' ? 'credentials' : 'qr');
  const [saved, setSaved] = useState(channel);
  const [qr, setQr] = useState<ChannelQr | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    if (!saved || qr?.status !== 'pending') return;
    const controller = new AbortController();
    const timer = window.setInterval(() => {
      void channelApi<ChannelQr>(agentId, workspaceId, `/${saved.id}/qr`)
        .then((next) => {
          if (controller.signal.aborted) return;
          setQr(next);
          if (next.status === 'bound') {
            setSaved({ ...saved, binding_status: 'bound' });
            onSaved();
          }
        })
        .catch((e) => {
          if (!controller.signal.aborted) {
            setError(errorMessage(e));
            setQr(null);
          }
        });
    }, 2500);
    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [agentId, workspaceId, saved, qr?.status, onSaved]);

  const submit = async (generateQr: boolean) => {
    if (environment.error) {
      setError(errorMessage(environment.error));
      return;
    }
    if (!environment.selectedEnvironment) {
      setError(msg('managedAgents.channels.environmentRequired', '请先选择执行环境，再保存渠道'));
      return;
    }
    const validationError = validateChannelInput(value, binding, saved, msg);
    if (validationError) {
      setError(validationError);
      return;
    }
    setBusy(true);
    setError('');
    try {
      const { app_id: appId, app_secret: appSecret, ...settings } = value;
      const credentials = binding === 'credentials' && appSecret ? { app_id: appId, app_secret: appSecret } : {};
      const next = await channelApi<Channel>(agentId, workspaceId, saved ? `/${saved.id}` : '', 'POST', {
        ...settings,
        ...credentials,
      });
      setSaved(next);
      setValue((current) => ({ ...current, app_secret: '' }));
      onSaved();
      if (generateQr) setQr(await channelApi<ChannelQr>(agentId, workspaceId, `/${next.id}/qr`, 'POST', {}));
      else onClose();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return { value, setValue, binding, setBinding, saved, qr, busy: busy || environment.isPending, error, submit };
}

export function validateChannelInput(
  value: ChannelInput,
  binding: string,
  saved: Channel | null,
  msg: ReturnType<typeof useI18n>['msg'],
) {
  if (!value.name.trim()) return msg('managedAgents.channels.nameRequired', '请输入机器人名称');
  if (binding !== 'credentials') return '';
  if (!value.app_id?.trim()) return msg('managedAgents.channels.appIdRequired', '请填写 App ID');
  const changingApp = saved?.app_id !== value.app_id.trim();
  if (!value.app_secret?.trim() && (saved?.binding_status !== 'bound' || changingApp))
    return msg('managedAgents.channels.secretRequired', '请填写 App Secret');
  return '';
}
