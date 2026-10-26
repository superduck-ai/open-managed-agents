import { useI18n } from '../../../shared/i18n';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '../../../shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../../shared/ui/dialog';
import { Badge } from '../../../shared/ui/badge';
import { channelApi, type Channel, type Pairing } from './api';
import { errorMessage } from '../utils';

export function PairingsDialog({
  agentId,
  workspaceId,
  channel,
  onClose,
  canEdit,
  finalFocus,
}: {
  agentId: string;
  workspaceId: string;
  channel: Channel;
  onClose: () => void;
  canEdit: boolean;
  finalFocus: () => HTMLElement | null;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const query = useQuery({
    queryKey: ['channel-pairings', workspaceId, agentId, channel.id],
    queryFn: () => channelApi<{ data: Pairing[] }>(agentId, workspaceId, `/${channel.id}/pairings`),
    refetchInterval: 5000,
  });
  const decide = async (pairing: Pairing, status: 'approved' | 'rejected') => {
    setBusy(true);
    setError('');
    try {
      await channelApi(agentId, workspaceId, `/${channel.id}/pairings/${pairing.id}`, 'POST', { status });
      await query.refetch();
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(open) => {
        if (!busy) setOpen(open);
      }}
      onOpenChangeComplete={(nextOpen) => {
        if (!nextOpen) onClose();
      }}
    >
      <DialogContent
        closeButtonDisabled={busy}
        finalFocus={finalFocus}
        className="max-h-[90vh] grid-rows-[auto_minmax(0,1fr)] sm:max-w-2xl"
      >
        <DialogHeader>
          <DialogTitle>
            {msg('managedAgents.channels.pairingsTitle', 'Pairing management ({name})', { name: channel.name })}
          </DialogTitle>
          <DialogDescription>
            {msg(
              'managedAgents.channels.pairingsHint',
              '批准后，该单聊或群聊可使用当前 Agent。撤销后将停止接受新的消息。',
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="min-h-0 space-y-3 overflow-y-auto">
          {(error || query.error) && (
            <p role="alert" className="text-sm text-destructive">
              {error || errorMessage(query.error)}
            </p>
          )}
          {query.isPending ? (
            <p>{msg('managedAgents.channels.pairingsLoading', '正在加载配对申请…')}</p>
          ) : query.isError ? null : (
            <div className="space-y-3">
              {query.data?.data.length ? (
                query.data.data.map((pairing) => (
                  <div
                    key={pairing.id}
                    className="flex flex-wrap items-center justify-between gap-3 rounded-lg border-[0.5px] border-border p-3"
                  >
                    <div className="min-w-0 space-y-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate font-medium">{pairing.name}</span>
                        <Badge variant="outline">
                          {pairing.kind === 'user'
                            ? msg('managedAgents.channels.directChat', '单聊')
                            : msg('managedAgents.channels.groupChat', '群聊')}
                        </Badge>
                      </div>
                      <p className="break-all text-xs text-muted-foreground">{pairing.target}</p>
                      <p className="text-xs">
                        {
                          {
                            pending: msg('managedAgents.channels.pending', '待审批'),
                            approved: msg('managedAgents.channels.approved', '已批准'),
                            rejected: msg('managedAgents.channels.rejected', '已拒绝'),
                          }[pairing.status]
                        }
                      </p>
                    </div>
                    {canEdit && (
                      <div className="flex gap-2">
                        {pairing.status !== 'approved' && (
                          <Button size="sm" disabled={busy} onClick={() => void decide(pairing, 'approved')}>
                            {msg('managedAgents.channels.approve', '批准')}
                          </Button>
                        )}
                        {pairing.status !== 'rejected' && (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busy}
                            onClick={() => void decide(pairing, 'rejected')}
                          >
                            {pairing.status === 'approved'
                              ? msg('managedAgents.channels.revoke', '撤销')
                              : msg('managedAgents.channels.reject', '拒绝')}
                          </Button>
                        )}
                      </div>
                    )}
                  </div>
                ))
              ) : (
                <p className="py-8 text-center text-sm text-muted-foreground">
                  {msg(
                    'managedAgents.channels.pairingsEmpty',
                    '暂无配对申请。用户给机器人发送私聊或在群中 @ 机器人后，申请会显示在这里。',
                  )}
                </p>
              )}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
