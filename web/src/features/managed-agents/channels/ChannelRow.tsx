import { useI18n } from '../../../shared/i18n';
import { Bot, MoreHorizontal, Settings2, Trash2 } from 'lucide-react';
import { Card } from '../../../shared/ui/card';
import { Button } from '../../../shared/ui/button';
import { Switch } from '../../../shared/ui/switch';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { type Channel } from './api';

export function ChannelRow({
  channel,
  canEdit,
  busy,
  onEdit,
  onPairing,
  onDelete,
  onToggle,
}: {
  channel: Channel;
  canEdit: boolean;
  busy: boolean;
  onEdit: () => void;
  onPairing: () => void;
  onDelete: () => void;
  onToggle: () => void;
}) {
  const { msg } = useI18n();
  const online = channel.connected && channel.enabled;
  return (
    <Card className="gap-0 overflow-hidden rounded-xl border border-border bg-background p-0 shadow-none ring-0 transition-colors hover:border-foreground/20">
      <div className="flex items-start gap-2.5 p-4 pb-3">
        <div className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-muted/40 text-muted-foreground">
          <Bot className="size-4" />
        </div>
        <div className="min-w-0 flex-1 space-y-1.5">
          <p className="truncate text-sm font-semibold" title={channel.name}>
            {channel.name}
          </p>
          <span
            className={
              online
                ? 'flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-400'
                : 'flex items-center gap-1.5 text-xs text-muted-foreground'
            }
          >
            <span className="size-1.5 rounded-full bg-current" />
            {channelStatus(channel, msg)}
          </span>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={msg('managedAgents.channels.moreActions', 'More actions for {name}', {
                  name: channel.name,
                })}
                disabled={!canEdit || busy}
              />
            }
          >
            <MoreHorizontal />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem variant="destructive" onClick={onDelete}>
              <Trash2 />
              {msg('managedAgents.channels.delete', '删除机器人')}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <dl className="space-y-2 px-4 pb-4 text-xs">
        <div className="flex items-center justify-between gap-4">
          <dt className="shrink-0 text-muted-foreground">App ID</dt>
          <dd className="truncate font-mono" title={channel.app_id}>
            {channel.app_id ?? msg('managedAgents.channels.waitingBinding', '等待绑定')}
          </dd>
        </div>
        <div className="flex items-center justify-between gap-4">
          <dt className="text-muted-foreground">{msg('managedAgents.channels.accessMode', '接入模式')}</dt>
          <dd>
            {channel.mode === 'open'
              ? msg('managedAgents.channels.open', '开放模式')
              : msg('managedAgents.channels.pairing', '配对模式')}
          </dd>
        </div>
      </dl>
      <div className="mt-auto flex flex-wrap items-center justify-between gap-2 border-t border-border bg-muted/20 px-4 py-2.5">
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <Switch
            aria-label={msg('managedAgents.channels.enableBot', 'Enable {name}', { name: channel.name })}
            disabled={!canEdit || busy}
            checked={channel.enabled}
            onCheckedChange={onToggle}
          />
          {msg('managedAgents.channels.enable', '启用')}
        </label>
        <div className="flex items-center gap-1">
          {channel.mode === 'pairing' && (
            <Button variant="ghost" size="sm" onClick={onPairing}>
              {msg('managedAgents.channels.pairings', 'Pairing management')}
              {channel.pending_pairings ? ` (${channel.pending_pairings})` : ''}
            </Button>
          )}
          <Button variant="outline" size="sm" disabled={!canEdit || busy} onClick={onEdit}>
            <Settings2 className="size-3.5" />
            {channel.binding_status === 'unbound'
              ? msg('managedAgents.channels.bind', '绑定')
              : msg('managedAgents.channels.configure', '配置')}
          </Button>
        </div>
      </div>
    </Card>
  );
}

function channelStatus(channel: Channel, msg: ReturnType<typeof useI18n>['msg']) {
  if (!channel.enabled) return msg('managedAgents.channels.disabled', '已停用');
  if (channel.binding_status !== 'bound') return msg('managedAgents.channels.unbound', '待绑定');
  return channel.connected
    ? msg('managedAgents.channels.connected', '已连接')
    : msg('managedAgents.channels.disconnected', '连接失败');
}
