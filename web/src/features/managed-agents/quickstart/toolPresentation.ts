import { CalendarClock, Cloud, LockKeyhole, Play, Search, Terminal } from 'lucide-react';
import { type I18nMsg, type IconComponent } from '../types';

export type QuickstartToolPresentation = {
  layout: 'card' | 'compact-status' | 'status-line';
  occupiesSpeaker: boolean;
};

export function quickstartToolPresentation(name: string): QuickstartToolPresentation {
  if (name === 'list_environments' || name === 'list_vaults') {
    return { layout: 'status-line', occupiesSpeaker: false };
  }
  if (name === 'flag_schedule_intent') {
    return { layout: 'compact-status', occupiesSpeaker: false };
  }
  return { layout: 'card', occupiesSpeaker: true };
}

export function quickstartToolMeta(name: string, msg: I18nMsg): { label: string; icon: IconComponent } {
  const t = (id: string, fallback: string) => msg(id, fallback);
  switch (name) {
    case 'list_environments':
      return { label: t('managedAgents.quickstart.toolMeta.listEnvironments', 'List environments'), icon: Cloud };
    case 'create_environment':
      return { label: t('managedAgents.quickstart.toolMeta.createEnvironment', 'Create environment'), icon: Cloud };
    case 'vault_sharing_notice':
      return {
        label: t('managedAgents.quickstart.toolMeta.vaultSharingNotice', 'Vault sharing notice'),
        icon: LockKeyhole,
      };
    case 'list_vaults':
      return { label: t('managedAgents.quickstart.toolMeta.listVaults', 'List vaults'), icon: LockKeyhole };
    case 'select_vault':
      return { label: t('managedAgents.quickstart.toolMeta.selectVault', 'Select vault'), icon: LockKeyhole };
    case 'create_vault':
      return { label: t('managedAgents.quickstart.toolMeta.createVault', 'Create vault'), icon: LockKeyhole };
    case 'create_vault_credential':
      return { label: t('managedAgents.quickstart.toolMeta.createCredential', 'Create credential'), icon: LockKeyhole };
    case 'flag_schedule_intent':
      return { label: t('managedAgents.quickstart.toolMeta.scheduleIntent', 'Schedule intent'), icon: CalendarClock };
    case 'create_deployment':
      return { label: t('managedAgents.quickstart.toolMeta.createDeployment', 'Create deployment'), icon: Play };
    case 'web_search':
      return { label: t('managedAgents.quickstart.toolMeta.searchWeb', 'Search web'), icon: Search };
    default:
      return { label: name.replace(/_/g, ' '), icon: Terminal };
  }
}
