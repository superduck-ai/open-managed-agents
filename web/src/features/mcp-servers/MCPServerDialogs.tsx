import { AlertCircle, RefreshCw, X } from 'lucide-react';
import { useState, type ReactNode } from 'react';

import {
  mcpServerErrorMessage,
  type MCPServerMutation,
  type WorkspaceMCPServer,
} from '../../shared/api/workspaceMCPServers';
import { useI18n } from '../../shared/i18n';
import { type MCPServerInputError, validateMCPServerInput } from '../../shared/lib/mcp-server-input';
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import { Input } from '../../shared/ui/input';
import { Field, FieldDescription, FieldError, FieldLabel } from '../../shared/ui/field';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '../../shared/ui/sheet';
import { MCPServerActionsMenu } from './MCPServersTable';

export type MCPServerDetailTarget = { serverId: string };
export type MCPServerEditorTarget = { mode: 'create' } | { mode: 'edit'; server: WorkspaceMCPServer };
export type MCPServerDestructiveTarget = { server: WorkspaceMCPServer };

export function MCPServerPanel({
  target,
  detailServer,
  detailLoading,
  detailError,
  formatter,
  onClose,
  onRetry,
  onEdit,
  onDelete,
}: {
  target: MCPServerDetailTarget | null;
  detailServer?: WorkspaceMCPServer;
  detailLoading: boolean;
  detailError: unknown;
  formatter: Intl.DateTimeFormat;
  onClose: () => void;
  onRetry: () => void;
  onEdit: (server: WorkspaceMCPServer) => void;
  onDelete: (server: WorkspaceMCPServer) => void;
}) {
  return (
    <Sheet open={Boolean(target)} onOpenChange={(open) => !open && onClose()}>
      <SheetContent showCloseButton={false} showOverlay={false} side="right" className="gap-0 p-0 sm:!max-w-md">
        <MCPServerDetailPanel
          server={detailServer}
          isLoading={detailLoading}
          error={detailError}
          formatter={formatter}
          onClose={onClose}
          onRetry={onRetry}
          onEdit={onEdit}
          onDelete={onDelete}
        />
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
  onDelete,
}: {
  server?: WorkspaceMCPServer;
  isLoading: boolean;
  error: unknown;
  formatter: Intl.DateTimeFormat;
  onClose: () => void;
  onRetry: () => void;
  onEdit: (server: WorkspaceMCPServer) => void;
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
            <SheetTitle className="truncate">{server.name}</SheetTitle>
            <SheetDescription className="mt-1 font-mono">{server.id}</SheetDescription>
          </div>
          <div className="absolute right-4 top-4 flex items-center gap-1">
            <MCPServerActionsMenu server={server} onEdit={onEdit} onDelete={onDelete} />
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
        </DetailSection>
        <DetailSection title={msg('mcpServers.lifecycle', 'Lifecycle')} bordered>
          <DetailRow label={msg('common.created', 'Created')} value={formatDate(server.created_at, formatter)} />
          <DetailRow label={msg('common.updated', 'Updated')} value={formatDate(server.updated_at, formatter)} />
        </DetailSection>
      </div>
    </>
  );
}

export function MCPServerEditor({
  target,
  onClose,
  onSubmit,
}: {
  target: MCPServerEditorTarget | null;
  onClose: () => void;
  onSubmit: (input: MCPServerMutation) => Promise<void>;
}) {
  if (!target) {
    return null;
  }
  const editorKey = target.mode === 'edit' ? `edit:${target.server.id}` : 'create';
  return <MCPServerEditorDialog key={editorKey} target={target} onClose={onClose} onSubmit={onSubmit} />;
}

function MCPServerEditorDialog({
  target,
  onClose,
  onSubmit,
}: {
  target: MCPServerEditorTarget;
  onClose: () => void;
  onSubmit: (input: MCPServerMutation) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [name, setName] = useState(target.mode === 'edit' ? target.server.name : '');
  const [url, setURL] = useState(target.mode === 'edit' ? target.server.url : '');
  const [inputErrors, setInputErrors] = useState<ReturnType<typeof validateMCPServerInput>>({});
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const editing = target.mode === 'edit';
  const title = editing ? msg('mcpServers.edit', 'Edit MCP server') : msg('mcpServers.create', 'Create MCP server');
  const submit = async () => {
    if (saving) return;
    const nextInputErrors = validateMCPServerInput(name, url);
    if (Object.keys(nextInputErrors).length > 0) {
      setInputErrors(nextInputErrors);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await onSubmit({ name: name.trim(), url: url.trim() });
    } catch (submitError) {
      setError(
        mcpServerErrorMessage(
          submitError,
          msg('mcpServers.errors.duplicate', 'An MCP server with this name or URL already exists.'),
        ),
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && !saving && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {msg(
              'mcpServers.editorDescription',
              'When an Agent selects this configuration, its name and endpoint are copied into the Agent configuration.',
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <Field data-invalid={Boolean(inputErrors.name)}>
            <FieldLabel htmlFor="mcp-server-name">
              {msg('managedAgents.agents.createDialog.customMcpName', 'Name')}
            </FieldLabel>
            <Input
              id="mcp-server-name"
              value={name}
              autoComplete="off"
              placeholder="internal-docs"
              aria-invalid={Boolean(inputErrors.name) || undefined}
              onChange={(event) => {
                setName(event.currentTarget.value);
                setInputErrors((current) => ({ ...current, name: undefined }));
              }}
            />
            <FieldError>{mcpInputError('name', inputErrors.name, msg)}</FieldError>
          </Field>
          <Field data-invalid={Boolean(inputErrors.url)}>
            <FieldLabel htmlFor="mcp-server-url">
              {msg('managedAgents.agents.createDialog.customMcpUrl', 'MCP Server URL')}
            </FieldLabel>
            <Input
              id="mcp-server-url"
              value={url}
              inputMode="url"
              autoComplete="url"
              placeholder="https://mcp.example.com/mcp"
              aria-invalid={Boolean(inputErrors.url) || undefined}
              onChange={(event) => {
                setURL(event.currentTarget.value);
                setInputErrors((current) => ({ ...current, url: undefined }));
              }}
            />
            <FieldDescription>
              {msg('managedAgents.agents.createDialog.customMcpUrlHint', 'Only HTTP and HTTPS URLs are supported.')}
            </FieldDescription>
            <FieldError>{mcpInputError('url', inputErrors.url, msg)}</FieldError>
          </Field>
          {error ? (
            <Alert variant="destructive">
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          ) : null}
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" disabled={saving} onClick={onClose}>
            {msg('common.cancel', 'Cancel')}
          </Button>
          <Button type="button" disabled={saving} onClick={() => void submit()}>
            {saving
              ? msg('common.saving', 'Saving...')
              : editing
                ? msg('common.save', 'Save')
                : msg('common.create', 'Create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function mcpInputError(
  field: 'name' | 'url',
  error: MCPServerInputError | undefined,
  msg: ReturnType<typeof useI18n>['msg'],
) {
  if (!error) return null;
  if (field === 'name') {
    switch (error) {
      case 'required':
        return msg('managedAgents.agents.createDialog.customMcpNameRequired', 'Name is required.');
      case 'too_long':
        return msg('managedAgents.agents.createDialog.customMcpNameTooLong', 'Name must be at most 255 characters.');
      case 'ambiguous':
        return msg(
          'managedAgents.agents.createDialog.customMcpNameAmbiguous',
          'Name must not contain consecutive underscores.',
        );
      default:
        return msg(
          'managedAgents.agents.createDialog.customMcpNameInvalid',
          'Use only letters, numbers, underscores, hyphens, or periods.',
        );
    }
  }
  switch (error) {
    case 'required':
      return msg('managedAgents.agents.createDialog.customMcpUrlRequired', 'MCP Server URL is required.');
    case 'too_long':
      return msg('managedAgents.agents.createDialog.customMcpUrlTooLong', 'MCP Server URL must be at most 2048 bytes.');
    default:
      return msg(
        'managedAgents.agents.createDialog.customMcpUrlInvalid',
        'Enter a valid HTTP or HTTPS MCP Server URL.',
      );
  }
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
  return (
    <AlertDialog open={Boolean(target)} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{msg('mcpServers.deleteTitle', 'Delete MCP server?')}</AlertDialogTitle>
          <AlertDialogDescription>
            {msg(
              'mcpServers.deleteDescription',
              'This removes the reusable configuration from the workspace directory. Existing Agents keep the configuration already copied into their versions.',
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
          <AlertDialogAction variant="destructive" disabled={isActing} onClick={onConfirm}>
            {msg('common.delete', 'Delete')}
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
