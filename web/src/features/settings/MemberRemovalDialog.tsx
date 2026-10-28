import { type ReactNode, useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';
import { Button } from '../../shared/ui/button';

export function MemberRemovalDialog({
  open,
  title,
  children,
  pending,
  error,
  onClose,
  onConfirm,
}: {
  open: boolean;
  title: string;
  children: ReactNode;
  pending: boolean;
  error?: string;
  onClose: () => void;
  onConfirm: () => void;
}) {
  const { msg } = useI18n();
  const [content, setContent] = useState({ title, children, error });
  if (open && (title !== content.title || children !== content.children || error !== content.error)) {
    setContent({ title, children, error });
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
      <AlertDialogContent className="grid-rows-[auto_minmax(0,1fr)_auto] max-h-[calc(100dvh-2rem)] overflow-clip">
        <AlertDialogHeader className="min-w-0">
          <AlertDialogTitle>{content.title}</AlertDialogTitle>
        </AlertDialogHeader>
        <div className="subtle-scrollbar min-h-0 min-w-0 space-y-4 overflow-y-auto">
          <AlertDialogDescription className="[overflow-wrap:anywhere]">{content.children}</AlertDialogDescription>
          {content.error ? (
            <Alert variant="destructive">
              <AlertDescription>{content.error}</AlertDescription>
            </Alert>
          ) : null}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{msg('members.cancel', 'Cancel')}</AlertDialogCancel>
          <Button variant="destructive" disabled={pending} onClick={onConfirm}>
            {pending ? msg('members.removing', 'Removing...') : msg('members.remove', 'Remove')}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
