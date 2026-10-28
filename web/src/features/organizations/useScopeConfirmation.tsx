import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { hasScopeUnsavedChanges } from '../../shared/organizations/unsaved';
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

export function useScopeConfirmation() {
  const { msg } = useI18n();
  const [pending, setPending] = useState<(() => void) | null>(null);
  const request = (action: () => void) => {
    if (hasScopeUnsavedChanges()) setPending(() => action);
    else action();
  };
  const dialog = (
    <AlertDialog
      open={Boolean(pending)}
      onOpenChange={(open) => {
        if (!open) setPending(null);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{msg('organizations.unsaved.title', 'Discard unsaved changes?')}</AlertDialogTitle>
          <AlertDialogDescription>
            {msg(
              'organizations.unsaved.description',
              'Switching organizations or workspaces closes this page and discards unsaved changes.',
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{msg('organizations.unsaved.keepEditing', 'Keep editing')}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              const action = pending;
              setPending(null);
              action?.();
            }}
          >
            {msg('organizations.unsaved.switch', 'Discard and switch')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
  return { request, dialog };
}
