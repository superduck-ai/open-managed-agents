import { Trash2 } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
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
import { FieldError } from '../../shared/ui/field';
import type { LLMProvider } from './api';

export function ProviderDeleteDialog({
  provider,
  error,
  isPending,
  onClose,
  onConfirm,
}: {
  provider: LLMProvider | null;
  error: string;
  isPending: boolean;
  onClose: () => void;
  onConfirm: (provider: LLMProvider) => void;
}) {
  const { msg } = useI18n();
  return (
    <AlertDialog
      open={Boolean(provider)}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogMedia>
            <Trash2 aria-hidden />
          </AlertDialogMedia>
          <AlertDialogTitle>{msg('llmModels.deleteTitle', 'Delete provider?')}</AlertDialogTitle>
          <AlertDialogDescription>
            {msg('llmModels.deleteDescription', 'Its models will stop working immediately in this workspace.')}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error ? <FieldError>{error}</FieldError> : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={isPending}
            onClick={() => {
              if (provider) onConfirm(provider);
            }}
          >
            {msg('common.delete', 'Delete')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
