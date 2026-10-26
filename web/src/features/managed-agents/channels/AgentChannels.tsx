import { useI18n } from '../../../shared/i18n';
import { useCallback, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { MessageCircle, Plus, RefreshCw } from 'lucide-react';
import { Button } from '../../../shared/ui/button';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../../shared/ui/dialog';
import feishuLogo from './feishu.ico';
import { ChannelRow } from './ChannelRow';
import { ChannelDialog } from './ChannelDialog';
import { PairingsDialog } from './PairingsDialog';
import { channelApi, type Channel } from './api';
import { errorMessage } from '../utils';

export function AgentChannels({
  agentId,
  workspaceId,
  canEdit,
}: {
  agentId: string;
  workspaceId: string;
  canEdit: boolean;
}) {
  const { msg } = useI18n();
  const focusTarget = useRef<HTMLElement | null>(null);
  const createButton = useRef<HTMLButtonElement | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [editing, setEditing] = useState<Channel | null | undefined>(undefined);
  const [pairing, setPairing] = useState<Channel | null>(null);
  const [deleting, setDeleting] = useState<Channel | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const query = useQuery({
    queryKey: ['agent-channels', workspaceId, agentId],
    queryFn: () => channelApi<{ data: Channel[] }>(agentId, workspaceId),
    refetchInterval: 10000,
  });
  const refresh = useCallback(() => {
    void query.refetch();
  }, [query.refetch]);
  const channels = query.data?.data ?? [];
  const consoleCount = channels.filter((channel) => channel.source === 'console').length;
  const mutate = async (channel: Channel, remove: boolean) => {
    setBusy(true);
    setError('');
    try {
      await channelApi(
        agentId,
        workspaceId,
        `/${channel.id}`,
        remove ? 'DELETE' : 'POST',
        remove ? undefined : { enabled: !channel.enabled },
      );
      setDeleteOpen(false);
      await query.refetch();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <section
      className="space-y-5 pb-8"
      aria-label={msg('managedAgents.channels.management', '渠道管理')}
      onClickCapture={(event) => {
        if (editing !== undefined || pairing || deleting) return;
        if (event.target instanceof Element) {
          const button = event.target.closest('button');
          if (button) focusTarget.current = button;
        }
      }}
    >
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="space-y-1.5">
          <p className="text-sm leading-6 text-muted-foreground">
            {msg('managedAgents.channels.scopeHint', '仅显示当前模板下创建的渠道。配对管理中批准申请时绑定当前模板。')}
          </p>
          <h2 className="pt-3 text-base font-semibold">{msg('managedAgents.channels.connections', 'IM 连接管理')}</h2>
        </div>
      </div>
      {(error || query.error) && (
        <Alert variant="destructive">
          <AlertDescription>
            {error || errorMessage(query.error)}
            <Button variant="outline" size="sm" onClick={refresh}>
              {msg('managedAgents.channels.retry', '重试')}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b-[0.5px] border-border px-5 py-4">
          <div className="flex items-center gap-3">
            <div className="flex size-9 items-center justify-center">
              <img src={feishuLogo} alt="" className="size-8" width={32} height={32} />
            </div>
            <div>
              <h3 className="text-sm font-semibold">{msg('managedAgents.channels.feishu', '飞书')}</h3>
              <p className="mt-0.5 text-xs text-muted-foreground">
                {msg('managedAgents.channels.description', '通过机器人接收消息与发送回复')}
              </p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={msg('managedAgents.channels.refresh', '刷新渠道')}
              onClick={refresh}
            >
              <RefreshCw className={query.isFetching ? 'animate-spin' : ''} />
            </Button>
            <Button
              size="sm"
              ref={createButton}
              disabled={!canEdit || consoleCount >= 5 || query.isPending || query.isError}
              onClick={() => setEditing(null)}
            >
              <Plus />
              {msg('managedAgents.channels.create', '创建机器人')}
            </Button>
          </div>
        </div>
        {query.isPending ? (
          <p className="p-8 text-center text-sm text-muted-foreground">
            {msg('managedAgents.channels.loading', '正在加载渠道…')}
          </p>
        ) : query.isError ? null : channels.length ? (
          <div className="grid gap-3 p-4 sm:grid-cols-2 xl:grid-cols-4">
            {channels.map((channel) => (
              <ChannelRow
                key={channel.id}
                channel={channel}
                canEdit={canEdit}
                busy={busy}
                onEdit={() => setEditing(channel)}
                onPairing={() => setPairing(channel)}
                onDelete={() => {
                  setDeleting(channel);
                  setDeleteOpen(true);
                }}
                onToggle={() => void mutate(channel, false)}
              />
            ))}
          </div>
        ) : (
          <div className="flex flex-col items-center gap-2 px-6 py-12 text-center">
            <MessageCircle className="mb-2 size-6 text-muted-foreground" />
            <p className="text-sm font-medium">{msg('managedAgents.channels.empty', '还没有连接机器人')}</p>
            <p className="max-w-sm text-sm leading-6 text-muted-foreground">
              {msg('managedAgents.channels.emptyHint', '创建飞书机器人并完成扫码，即可在飞书中与当前智能体对话。')}
            </p>
          </div>
        )}
        {query.data && (
          <div className="flex flex-wrap items-center justify-between gap-4 px-5 pb-4 text-xs text-muted-foreground">
            <span>{msg('managedAgents.channels.currentAgent', '仅连接当前智能体')}</span>
            <span className="flex flex-wrap gap-4 tabular-nums">
              {msg('managedAgents.channels.consoleCount', 'Console {count} / 5', { count: consoleCount })}
              {msg('managedAgents.channels.apiCount', 'Created via API {count}', {
                count: channels.length - consoleCount,
              })}
            </span>
          </div>
        )}
      </div>
      {editing !== undefined && (
        <ChannelDialog
          agentId={agentId}
          workspaceId={workspaceId}
          channel={editing}
          onClose={() => setEditing(undefined)}
          finalFocus={() => (focusTarget.current?.isConnected ? focusTarget.current : createButton.current)}
          onSaved={refresh}
        />
      )}
      {pairing && (
        <PairingsDialog
          agentId={agentId}
          workspaceId={workspaceId}
          channel={pairing}
          canEdit={canEdit}
          finalFocus={() => (focusTarget.current?.isConnected ? focusTarget.current : createButton.current)}
          onClose={() => {
            setPairing(null);
            refresh();
          }}
        />
      )}
      <Dialog
        open={deleteOpen}
        onOpenChange={(open) => {
          if (!busy) setDeleteOpen(open);
        }}
        onOpenChangeComplete={(open) => {
          if (!open) setDeleting(null);
        }}
      >
        <DialogContent
          closeButtonDisabled={busy}
          finalFocus={() => (focusTarget.current?.isConnected ? focusTarget.current : createButton.current)}
        >
          <DialogHeader>
            <DialogTitle>{msg('managedAgents.channels.delete', '删除机器人')}</DialogTitle>
            <DialogDescription>
              {msg(
                'managedAgents.channels.deleteHint',
                'Stop receiving messages for {name}. Existing OMA sessions will be kept.',
                { name: deleting?.name ?? '' },
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2">
            <Button variant="outline" disabled={busy} onClick={() => setDeleteOpen(false)}>
              {msg('managedAgents.channels.cancel', '取消')}
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => {
                if (deleting) void mutate(deleting, true);
              }}
            >
              {msg('managedAgents.channels.confirmDelete', '确认删除')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </section>
  );
}
