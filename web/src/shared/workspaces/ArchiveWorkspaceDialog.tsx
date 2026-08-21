import { AlertCircle, Archive, Loader2 } from 'lucide-react';
import { useState } from 'react';
import { useI18n } from '../i18n';
import { Alert, AlertDescription } from '../ui/alert';
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
} from '../ui/alert-dialog';
import type { Workspace } from './api';

type ArchiveWorkspaceDialogProps = {
  workspace: Workspace;
  onClose: () => void;
  onArchive: (workspaceId: string) => Promise<void>;
};

// ArchiveWorkspaceDialog confirms the irreversible archive action for a single
// workspace. The caller mounts it conditionally (only while a target workspace
// is selected) and owns dismissal via onClose. The dialog keeps its own
// submitting/error state so the confirm button can show progress and surface
// archive failures inline; mounting fresh on each open means that transient
// state resets without an effect.
export function ArchiveWorkspaceDialog({ workspace, onClose, onArchive }: ArchiveWorkspaceDialogProps) {
  const { msg } = useI18n();
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');

  const handleArchive = async () => {
    if (submitting) {
      return;
    }
    setSubmitting(true);
    setError('');
    try {
      await onArchive(workspace.id);
      // On success the caller unmounts this dialog via onClose. Leave
      // submitting set so the button keeps its "Archiving…" state until the
      // dialog is gone; resetting it here would flip the label back to
      // "Archive" for a frame before unmount and read as a flicker. The
      // failure path below resets submitting so the action can be retried.
      onClose();
    } catch (archiveError) {
      setError(
        archiveError instanceof Error && archiveError.message
          ? archiveError.message
          : msg('workspace.archive.error', 'Failed to archive workspace.'),
      );
      setSubmitting(false);
    }
  };

  return (
    <AlertDialog
      open
      onOpenChange={(next) => {
        // Block dismissal while the archive request is in flight so the user
        // sees the outcome (success close or inline error) instead of a stale row.
        if (!submitting && !next) {
          onClose();
        }
      }}
    >
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogMedia className="bg-destructive/10 text-destructive dark:bg-destructive/20">
            <Archive className="size-5" aria-hidden />
          </AlertDialogMedia>
          <AlertDialogTitle>{msg('workspace.archive.title', 'Archive workspace')}</AlertDialogTitle>
          <AlertDialogDescription>
            {msg(
              'workspace.archive.description',
              'Archiving deactivates the workspace and immediately revokes all of its API keys. This cannot be undone.',
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error ? (
          <Alert variant="destructive" className="mt-4">
            <AlertCircle className="size-4 shrink-0" aria-hidden />
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={submitting}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction
            type="button"
            variant="destructive"
            className="min-w-[82px]"
            onClick={() => void handleArchive()}
            disabled={submitting}
          >
            {submitting ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
            {submitting
              ? msg('workspace.archive.archiving', 'Archiving…')
              : msg('workspace.archive.confirm', 'Archive')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
