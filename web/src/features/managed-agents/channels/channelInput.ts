import type { useI18n } from '../../../shared/i18n';
import type { Channel, ChannelInput } from './api';

type WizardStep = 1 | 2 | 3;

export type WizardAccess = {
  binding: string;
  saved: Pick<Channel, 'binding_status' | 'app_id'> | null;
  qrStatus?: string | null;
};

type WizardFields = {
  name: string;
  agentId: string;
  environmentId: string;
  app_id?: string;
  app_secret?: string;
};

export function credentialFieldsReady(
  value: Pick<WizardFields, 'app_id' | 'app_secret'>,
  saved: WizardAccess['saved'],
) {
  if (!value.app_id?.trim()) return false;
  const changingApp = saved?.app_id !== value.app_id.trim();
  return Boolean(value.app_secret?.trim() || (saved?.binding_status === 'bound' && !changingApp));
}

export function channelWizardStepReady(step: WizardStep, value: WizardFields, access: WizardAccess) {
  if (step === 1) return stepConnectReady(value, access);
  if (step === 2) return true;
  return Boolean(value.agentId && value.environmentId);
}

function stepConnectReady(value: WizardFields, access: WizardAccess) {
  if (!value.name.trim()) return false;
  if (access.binding === 'credentials') return credentialFieldsReady(value, access.saved);
  return access.qrStatus === 'bound' || access.saved?.binding_status === 'bound';
}

export function wizardStepAllowed(target: WizardStep, value: WizardFields, access: WizardAccess) {
  if (target <= 1) return true;
  for (let step = 1; step < target; step += 1) {
    if (!channelWizardStepReady(step as WizardStep, value, access)) return false;
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
  if (!credentialFieldsReady(value, saved)) return msg('managedAgents.channels.secretRequired', '请填写 App Secret');
  return '';
}
