import type { useI18n } from '../../../shared/i18n';
import type { Channel, ChannelInput } from './api';

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
