import { Archive, MoreVertical, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from '../../../shared/ui/alert-dialog';
import { archiveManagedEntity, deleteManagedEntity } from '../api';
import type { EnvironmentApiResponse } from '../types';
import { environmentErrorMessage } from '../resources/environment-model';

export type EnvironmentAction = 'archive' | 'delete';
export type EnvironmentActionRequest = {
  action: EnvironmentAction;
  entities: EnvironmentApiResponse[];
  onCompleted?: () => void;
};

export function EnvironmentActions({
  entity,
  onAction,
}: {
  entity: EnvironmentApiResponse;
  onAction: (request: EnvironmentActionRequest) => void;
}) {
  const { msg } = useI18n();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon-sm" aria-label={msg('common.moreActions', 'More actions')} />}
      >
        <MoreVertical className="size-4" strokeWidth={1.5} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {!entity.archived_at && entity.state !== 'archived' ? (
          <DropdownMenuItem onClick={() => onAction({ action: 'archive', entities: [entity] })}>
            <Archive className="size-4" />
            {msg('common.archive', 'Archive')}
          </DropdownMenuItem>
        ) : null}
        <DropdownMenuItem variant="destructive" onClick={() => onAction({ action: 'delete', entities: [entity] })}>
          <Trash2 className="size-4" />
          {msg('common.delete', 'Delete')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function EnvironmentActionDialog({
  request,
  workspaceId,
  onClose,
  onChanged,
}: {
  request: EnvironmentActionRequest;
  workspaceId: string;
  onClose: () => void;
  onChanged: (ids: string[]) => void;
}) {
  const { msg } = useI18n();
  const [busy, setBusy] = useState(false);
  const [finished, setFinished] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const action = request.action === 'archive' ? msg('common.archive', 'Archive') : msg('common.delete', 'Delete');
  const run = async () => {
    if (busy) return;
    setBusy(true);
    setError(null);
    const completed: string[] = [];
    try {
      for (const entity of request.entities.filter((entity) => !finished.includes(entity.id))) {
        await (request.action === 'archive' ? archiveManagedEntity : deleteManagedEntity)(
          'environments',
          entity.id,
          workspaceId,
        );
        completed.push(entity.id);
      }
      request.onCompleted?.();
      onClose();
    } catch (cause) {
      setError(environmentErrorMessage(cause, request.action, msg));
    } finally {
      if (completed.length) {
        setFinished([...finished, ...completed]);
        onChanged(completed);
      }
      setBusy(false);
    }
  };
  return (
    <AlertDialog open onOpenChange={(open) => !open && !busy && onClose()}>
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogTitle>
            {msg(
              'environmentPage.confirmAction',
              '{action} {count, plural, one {environment} other {# environments}}?',
              { action, count: request.entities.length },
            )}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {request.action === 'archive'
              ? msg('environmentPage.archiveHelp', 'Archived environments can no longer be used for new sessions.')
              : msg(
                  'environmentPage.deleteHelp',
                  'This will permanently delete the selected environments. This action cannot be undone.',
                )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <Button
            variant={request.action === 'delete' ? 'destructive' : 'default'}
            disabled={busy}
            onClick={() => void run()}
          >
            {busy ? msg('managedAgents.common.working', 'Working...') : action}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
