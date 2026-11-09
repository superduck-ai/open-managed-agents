import type { useI18n } from '../../../shared/i18n';
import type { Channel } from './api';

export type ChannelConnectionStatus = 'connected' | 'pending_test' | 'abnormal' | 'disabled';

export function channelConnectionStatus(channel: Channel): ChannelConnectionStatus {
  if (!channel.enabled) return 'disabled';
  if (channel.binding_status !== 'bound') return 'pending_test';
  if (channel.connected) return 'connected';
  return 'abnormal';
}

export function channelConnectionStatusLabel(status: ChannelConnectionStatus, msg: ReturnType<typeof useI18n>['msg']) {
  switch (status) {
    case 'connected':
      return msg('managedAgents.channels.statusConnected', '已连通');
    case 'pending_test':
      return msg('managedAgents.channels.statusPendingTest', '待测试');
    case 'abnormal':
      return msg('managedAgents.channels.statusAbnormal', '异常');
    case 'disabled':
      return msg('managedAgents.channels.statusDisabled', '已停用');
  }
}

export function channelConnectionStatusClass(status: ChannelConnectionStatus) {
  switch (status) {
    case 'connected':
      return 'text-success';
    case 'pending_test':
      return 'text-amber-600 dark:text-amber-400';
    case 'abnormal':
      return 'text-destructive';
    case 'disabled':
      return 'text-muted-foreground';
  }
}
