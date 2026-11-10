import type { useI18n } from '../../../shared/i18n';
import type { Channel, ChannelInput } from './api';

type WizardStep = 1 | 2 | 3;

export function channelWizardStepReady(
  step: WizardStep,
  value: { name: string; agentId: string; environmentId: string },
) {
  if (step === 1) return Boolean(value.name.trim());
  if (step === 2) return true;
  return Boolean(value.agentId && value.environmentId);
}

export function wizardStepAllowed(target: WizardStep, value: { name: string; agentId: string; environmentId: string }) {
  if (target <= 1) return true;
  for (let step = 1; step < target; step += 1) {
    if (!channelWizardStepReady(step as WizardStep, value)) return false;
  }
  return true;
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
