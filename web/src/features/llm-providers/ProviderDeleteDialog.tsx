import { Trash2 } from 'lucide-react';
import { useState } from 'react';
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
  const [content, setContent] = useState({ provider, error });
  if (provider && (provider !== content.provider || error !== content.error)) {
    setContent({ provider, error });
  }
  return (
    <AlertDialog
      open={Boolean(provider)}
      onOpenChange={(open, details) => {
        if (!open && isPending) {
          details.cancel();
          return;
        }
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
            {content.provider?.scope === 'organization'
              ? msg(
                  'llmModels.deleteOrganizationDescription',
                  'This removes the organization default for every workspace. Workspace providers remain available. Models referenced through this organization provider will stop working.',
                )
              : msg('llmModels.deleteDescription', 'Its models will stop working immediately in this workspace.')}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {content.error ? <FieldError>{content.error}</FieldError> : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isPending}>{msg('common.cancel', 'Cancel')}</AlertDialogCancel>
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
