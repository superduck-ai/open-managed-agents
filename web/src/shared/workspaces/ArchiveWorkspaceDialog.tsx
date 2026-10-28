import { useI18n } from '../i18n';
import { useState } from 'react';
import {
  AlertDialog,
  AlertDialogCancel,
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
  const [content, setContent] = useState({ workspaceName, error });
  if (open && (workspaceName !== content.workspaceName || error !== content.error)) {
    setContent({ workspaceName, error });
  }
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next, details) => {
        if (!next && pending) {
          details.cancel();
          return;
        }
        if (!next) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {msg('workspace.archive.title', 'Archive {name}', { name: content.workspaceName })}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {msg(
              'workspace.archive.body',
              'Are you sure you want to archive the “{name}” workspace and all resources associated with it?',
              { name: content.workspaceName },
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {content.error ? (
          <p role="alert" className="text-sm text-destructive">
            {content.error}
          </p>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
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
