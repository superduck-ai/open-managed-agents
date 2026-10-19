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
import { channelStatus, type Channel } from './api';

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
            {channelStatus(channel)}
          </span>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={`${channel.name}更多操作`}
                disabled={!canEdit || busy}
              />
            }
          >
            <MoreHorizontal />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem variant="destructive" onClick={onDelete}>
              <Trash2 />
              删除机器人
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <dl className="space-y-2 px-4 pb-4 text-xs">
        <div className="flex items-center justify-between gap-4">
          <dt className="shrink-0 text-muted-foreground">App ID</dt>
          <dd className="truncate font-mono" title={channel.app_id}>
            {channel.app_id ?? '等待绑定'}
          </dd>
        </div>
        <div className="flex items-center justify-between gap-4">
          <dt className="text-muted-foreground">接入模式</dt>
          <dd>{channel.mode === 'open' ? '开放模式' : '配对模式'}</dd>
        </div>
      </dl>
      <div className="mt-auto flex flex-wrap items-center justify-between gap-2 border-t border-border bg-muted/20 px-4 py-2.5">
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <Switch
            aria-label={`${channel.name}启用`}
            disabled={!canEdit || busy}
            checked={channel.enabled}
            onCheckedChange={onToggle}
          />
          启用
        </label>
        <div className="flex items-center gap-1">
          {channel.mode === 'pairing' && (
            <Button variant="ghost" size="sm" onClick={onPairing}>
              配对管理{channel.pending_pairings ? ` (${channel.pending_pairings})` : ''}
            </Button>
          )}
          <Button variant="outline" size="sm" disabled={!canEdit || busy} onClick={onEdit}>
            <Settings2 className="size-3.5" />
            {channel.binding_status === 'unbound' ? '绑定' : '配置'}
          </Button>
        </div>
      </div>
    </Card>
  );
}
