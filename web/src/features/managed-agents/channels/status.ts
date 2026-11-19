import type { useI18n } from '../../../shared/i18n';
import type { Channel } from './api';

export type ChannelConnectionStatus = 'connected' | 'connecting' | 'pending_test' | 'abnormal' | 'disabled';

const initialConnectionWindowMs = 30_000;

export function channelConnectionStatus(channel: Channel, observedAt = Date.now()): ChannelConnectionStatus {
  if (!channel.enabled) return 'disabled';
  if (channel.binding_status !== 'bound') return 'pending_test';
  if (channel.connected) return 'connected';
  const deadline = channelConnectionDeadline(channel);
  return deadline !== undefined && observedAt < deadline ? 'connecting' : 'abnormal';
}

export function channelConnectionDeadline(channel: Channel) {
  if (!channel.enabled || channel.binding_status !== 'bound' || channel.connected) return;
  if (channel.connection_status === 'disconnected') return;
  const createdAt = Date.parse(channel.created_at ?? '');
  if (Number.isFinite(createdAt)) return createdAt + initialConnectionWindowMs;
}

export function channelConnectionStatusLabel(status: ChannelConnectionStatus, msg: ReturnType<typeof useI18n>['msg']) {
  switch (status) {
    case 'connected':
      return msg('managedAgents.channels.statusConnected', '已连通');
    case 'connecting':
      return msg('managedAgents.channels.statusConnecting', '连接中');
    case 'pending_test':
      return msg('managedAgents.channels.statusPendingTest', '待测试');
    case 'abnormal':
      return msg('managedAgents.channels.statusAbnormal', '异常');
    case 'disabled':
      return msg('managedAgents.channels.statusDisabled', '已停用');
  }
}

export function channelConnectionStatusNotice(status: ChannelConnectionStatus, msg: ReturnType<typeof useI18n>['msg']) {
  switch (status) {
    case 'connected':
      return msg('managedAgents.channels.statusNoticeConnected', '当前状态：已连通');
    case 'connecting':
      return msg('managedAgents.channels.statusNoticeConnecting', '正在建立渠道连接');
    case 'pending_test':
      return msg('managedAgents.channels.statusNoticePending', '当前状态：尚未完成绑定');
    case 'abnormal':
      return msg('managedAgents.channels.statusNoticeAbnormal', '当前状态：未连通');
    case 'disabled':
      return msg('managedAgents.channels.statusNoticeDisabled', '当前状态：已停用');
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
    case 'connecting':
    case 'disabled':
      return 'text-muted-foreground';
  }
}
