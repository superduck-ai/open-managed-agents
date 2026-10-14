import { useI18n } from '../i18n';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../ui/alert-dialog';
import { Button } from '../ui/button';

export function ArchiveWorkspaceDialog({
  open,
  workspaceName,
  pending,
  error,
  onClose,
  onConfirm,
}: {
  open: boolean;
  workspaceName: string;
  pending: boolean;
  error?: string;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {msg('workspace.archive.title', 'Archive {name}', { name: workspaceName })}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {msg(
              'workspace.archive.body',
              'Are you sure you want to archive the “{name}” workspace and all resources associated with it?',
              { name: workspaceName },
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error ? <p className="text-sm text-destructive">{error}</p> : null}
        <AlertDialogFooter>
          <Button variant="outline" disabled={pending} onClick={onClose}>
            {msg('common.cancel', 'Cancel')}
          </Button>
          <Button variant="destructive" disabled={pending} onClick={onConfirm}>
            {pending
              ? msg('workspace.archive.archiving', 'Archiving...')
              : msg('workspace.archive.confirm', 'Archive workspace')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
