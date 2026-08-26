import { Archive, Loader2, Plus, RotateCw } from 'lucide-react';
import { type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { cn } from '../../shared/lib/utils';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { CopyButton } from '../../shared/ui/copy-button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import type { McpTunnel, TunnelConnectionState, TunnelProbeResult } from './api';

export type PendingTunnelAction = { type: 'rotate' | 'archive'; tunnel: McpTunnel };

export function ConnectionBadge({ state }: { state: TunnelConnectionState }) {
  const { msg } = useI18n();
  const label =
    state === 'connected'
      ? msg('mcpTunnels.connection.connected', 'Connected')
      : state === 'disconnected'
        ? msg('mcpTunnels.connection.disconnected', 'Disconnected')
        : msg('mcpTunnels.connection.unknown', 'Unknown');
  return (
    <Badge
      variant="secondary"
      className={cn(
        'h-6 rounded-md px-2 text-xs font-medium',
        state === 'connected' ? 'status-success' : 'text-secondary-foreground',
      )}
    >
      {label}
    </Badge>
  );
}

export function TunnelReadinessBadge({ tunnel }: { tunnel: McpTunnel }) {
  const { msg } = useI18n();
  if (tunnel.archived_at) {
    return <Badge variant="secondary">{msg('common.archived', 'Archived')}</Badge>;
  }
  if (tunnel.connection.state === 'unknown') {
    return <Badge variant="secondary">{msg('mcpTunnels.readiness.unknown', 'Status unavailable')}</Badge>;
  }
  if (tunnel.connection.state === 'disconnected') {
    return <Badge variant="secondary">{msg('mcpTunnels.readiness.waiting', 'Waiting for connector')}</Badge>;
  }
  if (!tunnel.connection.channels.length) {
    return <Badge variant="secondary">{msg('mcpTunnels.readiness.noChannels', 'Connected, no channels')}</Badge>;
  }
  return <Badge className="status-success">{msg('mcpTunnels.readiness.ready', 'Ready')}</Badge>;
}

export function CreateTunnelDialog({
  open,
  displayName,
  busy,
  onOpenChange,
  onDisplayNameChange,
  onSubmit,
}: {
  open: boolean;
  displayName: string;
  busy: boolean;
  onOpenChange: (open: boolean) => void;
  onDisplayNameChange: (value: string) => void;
  onSubmit: (event: FormEvent) => void;
}) {
  const { msg } = useI18n();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form className="grid gap-4" onSubmit={onSubmit}>
          <DialogHeader>
            <DialogTitle>{msg('mcpTunnels.create.title', 'Create MCP tunnel')}</DialogTitle>
            <DialogDescription>
              {msg(
                'mcpTunnels.create.description',
                'The token will be revealed after creation and remains available from the tunnel detail page.',
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="tunnel-display-name">{msg('common.name', 'Name')}</Label>
            <Input
              id="tunnel-display-name"
              value={displayName}
              maxLength={255}
              placeholder={msg('mcpTunnels.create.namePlaceholder', 'Local tools')}
              onChange={(event) => onDisplayNameChange(event.target.value)}
              autoFocus
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? <Loader2 className="animate-spin" /> : <Plus />}
              {msg('common.create', 'Create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function ProbeDialog({
  probe,
  onClose,
}: {
  probe: { tunnel: McpTunnel; result: TunnelProbeResult } | null;
  onClose: () => void;
}) {
  const { msg } = useI18n();
  if (!probe) return null;
  const { result, tunnel } = probe;
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{msg('mcpTunnels.probe.title', 'MCP connection test passed')}</DialogTitle>
          <DialogDescription>
            {msg('mcpTunnels.probe.description', '{name} responded through the tunnel.', {
              name: tunnel.display_name || tunnel.id,
            })}
          </DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[140px_minmax(0,1fr)] gap-x-4 gap-y-2 rounded-lg border p-4 text-sm">
          <dt className="text-muted-foreground">{msg('mcpTunnels.probe.channel', 'Channel')}</dt>
          <dd>
            <code>{result.channel}</code>
          </dd>
          <dt className="text-muted-foreground">{msg('mcpTunnels.probe.server', 'Server')}</dt>
          <dd>{[result.server_name, result.server_version].filter(Boolean).join(' · ') || '—'}</dd>
          <dt className="text-muted-foreground">{msg('mcpTunnels.probe.protocol', 'Protocol')}</dt>
          <dd>
            <code>{result.protocol_version || '—'}</code>
          </dd>
          <dt className="text-muted-foreground">{msg('mcpTunnels.probe.tools', 'Tools')}</dt>
          <dd>{result.tools.length}</dd>
        </dl>
        <div className="max-h-56 overflow-auto rounded-lg bg-muted p-3">
          {result.tools.length ? (
            <ul className="space-y-2">
              {result.tools.map((tool) => (
                <li key={tool.name} className="text-sm">
                  <code>{tool.name}</code>
                  {tool.description ? <p className="mt-0.5 text-xs text-muted-foreground">{tool.description}</p> : null}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-sm text-muted-foreground">
              {msg('mcpTunnels.probe.noTools', 'The server reported no tools.')}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button onClick={onClose}>{msg('common.done', 'Done')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ConfirmTunnelActionDialog({
  action,
  busy,
  onClose,
  onConfirm,
}: {
  action: PendingTunnelAction | null;
  busy: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  const rotating = action?.type === 'rotate';
  return (
    <AlertDialog open={Boolean(action)} onOpenChange={(open) => !open && !busy && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>{rotating ? <RotateCw /> : <Archive />}</AlertDialogMedia>
          <AlertDialogTitle>
            {rotating
              ? msg('mcpTunnels.confirm.rotateTitle', 'Rotate tunnel token?')
              : msg('mcpTunnels.confirm.archiveTitle', 'Archive MCP tunnel?')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {rotating
              ? msg(
                  'mcpTunnels.confirm.rotateBody',
                  'The current tunnel-client credential will stop working immediately.',
                )
              : msg(
                  'mcpTunnels.confirm.archiveBody',
                  'Archived tunnels reject Connector requests and cannot be restored in this release.',
                )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction variant={rotating ? 'default' : 'destructive'} disabled={busy} onClick={onConfirm}>
            {busy ? <Loader2 className="animate-spin" /> : null}
            {rotating ? msg('mcpTunnels.action.rotateToken', 'Rotate token') : msg('common.archive', 'Archive')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function CopyValue({ label, value }: { label: string; value: string }) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-2">
      <Label>{label}</Label>
      <div className="flex min-w-0 items-center gap-2 rounded-lg border bg-muted/40 p-2">
        <code className="min-w-0 flex-1 break-all text-xs">{value}</code>
        <CopyButton value={value} label={msg('mcpTunnels.copyValue', 'Copy {label}', { label })} />
      </div>
    </div>
  );
}
