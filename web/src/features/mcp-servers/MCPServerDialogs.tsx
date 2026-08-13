import { useEffect, useState } from 'react';
import { useI18n } from '../../shared/i18n';
import {
  mcpServerErrorMessage,
  type MCPServerMutation,
  type WorkspaceMCPServer,
} from '../../shared/api/workspaceMCPServers';
import { Alert, AlertDescription } from '../../shared/ui/alert';
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
import { Label } from '../../shared/ui/label';

export type MCPServerEditorTarget = { mode: 'create' } | { mode: 'edit'; server: WorkspaceMCPServer };
export type MCPServerDestructiveTarget = { action: 'archive' | 'delete'; server: WorkspaceMCPServer };

export function MCPServerDetailError({
  enabled,
  error,
  onRetry,
}: {
  enabled: boolean;
  error: unknown;
  onRetry: () => void;
}) {
  const { msg } = useI18n();
  if (!enabled || !error) {
    return null;
  }
  return (
    <Alert variant="destructive">
      <AlertDescription>
        {mcpServerErrorMessage(error)}{' '}
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          {msg('common.retry', 'Retry')}
        </Button>
      </AlertDescription>
    </Alert>
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
  const { msg } = useI18n();
  const [name, setName] = useState('');
  const [url, setURL] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setName(target?.mode === 'edit' ? target.server.name : '');
    setURL(target?.mode === 'edit' ? target.server.url : '');
    setError(null);
  }, [target]);

  const title =
    target?.mode === 'edit' ? msg('mcpServers.edit', 'Edit MCP server') : msg('mcpServers.create', 'Create MCP server');
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
    <Dialog open={Boolean(target)} onOpenChange={(open) => !open && !saving && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {msg(
              'mcpServers.editorDescription',
              'Agents can select this configuration and copy its name and endpoint into their next version.',
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
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
        <DialogFooter>
          <Button type="button" variant="outline" disabled={saving} onClick={onClose}>
            {msg('common.cancel', 'Cancel')}
          </Button>
          <Button type="button" disabled={!name.trim() || !url.trim() || saving} onClick={() => void submit()}>
            {saving
              ? msg('common.saving', 'Saving...')
              : target?.mode === 'edit'
                ? msg('common.save', 'Save')
                : msg('common.create', 'Create')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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
