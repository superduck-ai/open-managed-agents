import { AlertCircle, RefreshCw, X } from 'lucide-react';
import { useEffect, useState, type ReactNode } from 'react';

import {
  mcpServerErrorMessage,
  type MCPServerMutation,
  type WorkspaceMCPServer,
} from '../../shared/api/workspaceMCPServers';
import { useI18n } from '../../shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import { MCPServerActionsMenu, MCPServerStatusBadge } from './MCPServersTable';

export type MCPServerPanelTarget =
  { mode: 'create' } | { mode: 'detail'; serverId: string } | { mode: 'edit'; server: WorkspaceMCPServer };
export type MCPServerDestructiveTarget = { action: 'archive' | 'delete'; server: WorkspaceMCPServer };

export function MCPServerPanel({
  target,
  detailServer,
  detailLoading,
  detailError,
  formatter,
  onClose,
  onRetry,
  onEdit,
  onArchive,
  onDelete,
  onShowDetail,
  onSubmit,
}: {
  target: MCPServerPanelTarget | null;
  detailServer?: WorkspaceMCPServer;
  detailLoading: boolean;
  detailError: unknown;
  formatter: Intl.DateTimeFormat;
  onClose: () => void;
  onRetry: () => void;
  onEdit: (server: WorkspaceMCPServer) => void;
  onArchive: (server: WorkspaceMCPServer) => void;
  onDelete: (server: WorkspaceMCPServer) => void;
  onShowDetail: (serverId: string) => void;
  onSubmit: (input: MCPServerMutation) => Promise<void>;
}) {
  const server = target?.mode === 'edit' ? target.server : detailServer;
  return (
    <Sheet open={Boolean(target)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent showCloseButton={false} showOverlay={false} side="right" className="gap-0 p-0 sm:!max-w-md">
        {target?.mode === 'create' || target?.mode === 'edit' ? (
          <MCPServerFormPanel
            target={target}
            onClose={onClose}
            onCancel={() => (target.mode === 'edit' ? onShowDetail(target.server.id) : onClose())}
            onSubmit={onSubmit}
          />
        ) : (
          <MCPServerDetailPanel
            server={server}
            isLoading={detailLoading}
            error={detailError}
            formatter={formatter}
            onClose={onClose}
            onRetry={onRetry}
            onEdit={onEdit}
            onArchive={onArchive}
            onDelete={onDelete}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function MCPServerDetailPanel({
  server,
  isLoading,
  error,
  formatter,
  onClose,
  onRetry,
  onEdit,
  onArchive,
  onDelete,
}: {
  server?: WorkspaceMCPServer;
  isLoading: boolean;
  error: unknown;
  formatter: Intl.DateTimeFormat;
  onClose: () => void;
  onRetry: () => void;
  onEdit: (server: WorkspaceMCPServer) => void;
  onArchive: (server: WorkspaceMCPServer) => void;
  onDelete: (server: WorkspaceMCPServer) => void;
}) {
  const { msg } = useI18n();
  if (isLoading) {
    return (
      <div className="p-4 text-sm text-muted-foreground">
        <RefreshCw className="mr-2 inline size-4 animate-spin" aria-hidden />
        {msg('mcpServers.detail.loading', 'Loading MCP server...')}
      </div>
    );
  }
  if (error || !server) {
    return (
      <div className="p-4">
        <Alert variant="destructive">
          <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <AlertTitle>{msg('mcpServers.detail.error', 'MCP server could not be loaded.')}</AlertTitle>
          <AlertDescription>
            <p>{mcpServerErrorMessage(error)}</p>
            <Button type="button" size="sm" variant="outline" className="mt-3" onClick={onRetry}>
              <RefreshCw className="size-3.5" aria-hidden />
              {msg('common.retry', 'Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      </div>
    );
  }
  return (
    <>
      <SheetHeader className="border-b border-border px-4 py-4 pr-12">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <SheetTitle className="truncate">{server.name}</SheetTitle>
              <MCPServerStatusBadge status={server.status} />
            </div>
            <SheetDescription className="mt-1 font-mono">{server.id}</SheetDescription>
          </div>
          <div className="absolute right-4 top-4 flex items-center gap-1">
            <MCPServerActionsMenu server={server} onEdit={onEdit} onArchive={onArchive} onDelete={onDelete} />
            <PanelCloseButton onClose={onClose} />
          </div>
        </div>
      </SheetHeader>
      <div className="subtle-scrollbar flex-1 space-y-5 overflow-y-auto px-4 py-4">
        <DetailSection title={msg('mcpServers.endpointUrl', 'Endpoint URL')}>
          <p className="break-all font-mono text-sm leading-6 text-foreground">{server.url}</p>
        </DetailSection>
        <DetailSection title={msg('mcpServers.configuration', 'Configuration')} bordered>
          <DetailRow label={msg('common.name', 'Name')} value={server.name} />
          <DetailRow label={msg('mcpServers.transport', 'Transport')} value={server.transport_type} mono />
          <DetailRow label={msg('mcpServers.status', 'Status')} value={server.status} />
        </DetailSection>
        <DetailSection title={msg('mcpServers.lifecycle', 'Lifecycle')} bordered>
          <DetailRow label={msg('common.created', 'Created')} value={formatDate(server.created_at, formatter)} />
          <DetailRow label={msg('common.updated', 'Updated')} value={formatDate(server.updated_at, formatter)} />
          {server.archived_at ? (
            <DetailRow
              label={msg('mcpServers.archivedAt', 'Archived')}
              value={formatDate(server.archived_at, formatter)}
            />
          ) : null}
        </DetailSection>
      </div>
    </>
  );
}

function MCPServerFormPanel({
  target,
  onClose,
  onCancel,
  onSubmit,
}: {
  target: Extract<MCPServerPanelTarget, { mode: 'create' | 'edit' }>;
  onClose: () => void;
  onCancel: () => void;
  onSubmit: (input: MCPServerMutation) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [name, setName] = useState('');
  const [url, setURL] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setName(target.mode === 'edit' ? target.server.name : '');
    setURL(target.mode === 'edit' ? target.server.url : '');
    setError(null);
  }, [target]);

  const editing = target.mode === 'edit';
  const title = editing ? msg('mcpServers.edit', 'Edit MCP server') : msg('mcpServers.create', 'Create MCP server');
  const submit = async () => {
    if (!name.trim() || !url.trim() || saving) return;
    setSaving(true);
    setError(null);
    try {
      await onSubmit({ name: name.trim(), url: url.trim() });
    } catch (submitError) {
      setError(mcpServerErrorMessage(submitError));
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <SheetHeader className="border-b border-border px-4 py-4 pr-12">
        <SheetTitle>{title}</SheetTitle>
        <SheetDescription>
          {msg(
            'mcpServers.editorDescription',
            'Agents can select this configuration and copy its name and endpoint into their next version.',
          )}
        </SheetDescription>
        <div className="absolute right-4 top-4">
          <PanelCloseButton disabled={saving} onClose={onClose} />
        </div>
      </SheetHeader>
      <div className="subtle-scrollbar flex-1 space-y-4 overflow-y-auto px-4 py-4">
        <div className="space-y-2">
          <Label htmlFor="mcp-server-name">{msg('mcpServers.name', 'Name')}</Label>
          <Input
            id="mcp-server-name"
            value={name}
            autoComplete="off"
            placeholder="internal-docs"
            onChange={(event) => setName(event.currentTarget.value)}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="mcp-server-url">{msg('mcpServers.endpointUrl', 'Endpoint URL')}</Label>
          <Input
            id="mcp-server-url"
            value={url}
            type="url"
            autoComplete="url"
            placeholder="https://mcp.example.com/mcp"
            onChange={(event) => setURL(event.currentTarget.value)}
          />
        </div>
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
      </div>
      <div className="flex justify-end gap-2 border-t border-border px-4 py-4">
        <Button type="button" variant="outline" disabled={saving} onClick={onCancel}>
          {msg('common.cancel', 'Cancel')}
        </Button>
        <Button type="button" disabled={!name.trim() || !url.trim() || saving} onClick={() => void submit()}>
          {saving
            ? msg('common.saving', 'Saving...')
            : editing
              ? msg('common.save', 'Save')
              : msg('common.create', 'Create')}
        </Button>
      </div>
    </>
  );
}

function DetailSection({
  title,
  bordered = false,
  children,
}: {
  title: string;
  bordered?: boolean;
  children: ReactNode;
}) {
  return (
    <section className={bordered ? 'space-y-3 border-t border-border pt-4' : 'space-y-2'}>
      <h2 className="text-sm font-medium text-foreground">{title}</h2>
      {children}
    </section>
  );
}

function DetailRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid grid-cols-[112px_minmax(0,1fr)] gap-3 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className={mono ? 'truncate font-mono text-foreground' : 'truncate text-foreground'} title={value}>
        {value}
      </span>
    </div>
  );
}

function PanelCloseButton({ disabled = false, onClose }: { disabled?: boolean; onClose: () => void }) {
  const { msg } = useI18n();
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      disabled={disabled}
      aria-label={msg('common.close', 'Close')}
      onClick={onClose}
    >
      <X className="size-4" aria-hidden />
    </Button>
  );
}

export function MCPServerDestructiveDialog({
  target,
  error,
  isActing,
  onClose,
  onConfirm,
}: {
  target: MCPServerDestructiveTarget | null;
  error: string | null;
  isActing: boolean;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  const archive = target?.action === 'archive';
  return (
    <AlertDialog open={Boolean(target)} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {archive
              ? msg('mcpServers.archiveTitle', 'Archive MCP server?')
              : msg('mcpServers.deleteTitle', 'Delete MCP server?')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {archive
              ? msg(
                  'mcpServers.archiveDescription',
                  'Archived servers stop appearing in Agent selection, but existing Agent versions keep their copied configuration.',
                )
              : msg(
                  'mcpServers.deleteDescription',
                  'This removes the reusable configuration. Existing Agent versions keep their copied configuration.',
                )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isActing}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction variant={archive ? 'default' : 'destructive'} disabled={isActing} onClick={onConfirm}>
            {archive ? msg('common.archive', 'Archive') : msg('common.delete', 'Delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function formatDate(value: string, formatter: Intl.DateTimeFormat) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : formatter.format(date);
}
