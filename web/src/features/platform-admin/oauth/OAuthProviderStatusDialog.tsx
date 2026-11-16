import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { updateOAuthProvider, type OAuthProviderRecord } from '../../../shared/api/platform-admin';
import type { ApiError } from '../../../shared/api/client';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import { usePlatformAdmin } from '../context';
import { usePlatformAdminError } from '../usePlatformAdminError';

const statusChanges = {
  enable: { enabled: true },
  disable: { enabled: false },
  archive: { archived: true },
  restore: { archived: false },
};

export type OAuthProviderStatusAction = keyof typeof statusChanges;

const actionTitles = {
  enable: 'Enable OAuth client?',
  disable: 'Disable OAuth client?',
  archive: 'Archive OAuth client?',
  restore: 'Restore OAuth client?',
};

const actionBodies = {
  enable: 'Show in the workspace provider catalog for creating new credentials.',
  disable:
    'Hide from the workspace provider catalog. Existing credentials and sessions remain available; disabling does not revoke authorization or automatic refresh.',
  archive:
    'Hide from the active list and workspace provider catalog. Existing credentials and automatic refresh remain available.',
  restore: 'Restore the client with its previous enabled or disabled state.',
};

export function OAuthProviderStatusDialog({
  provider,
  action,
  onClose,
  onSaved,
  onConflict,
}: {
  provider: OAuthProviderRecord;
  action: OAuthProviderStatusAction;
  onClose: () => void;
  onSaved: () => Promise<void>;
  onConflict: () => Promise<void>;
}) {
  const { client } = usePlatformAdmin();
  const { msg } = useI18n();
  const errorMessage = usePlatformAdminError();
  const [error, setError] = useState<string | null>(null);
  const mutation = useMutation({
    mutationFn: () =>
      updateOAuthProvider(client, provider.id, { ...statusChanges[action], expected_version: provider.version }),
    onSuccess: onSaved,
    onError: async (cause: unknown) => {
      setError(errorMessage(cause));
      if ((cause as ApiError).code === 'oauth_provider_version_conflict') await onConflict();
    },
  });
  return (
    <Dialog open onOpenChange={(open) => !open && !mutation.isPending && onClose()}>
      <DialogContent className="sm:max-w-md" closeButtonDisabled={mutation.isPending}>
        <DialogHeader>
          <DialogTitle>{msg(`platformAdmin.oauthManagement.${action}Title`, actionTitles[action])}</DialogTitle>
          <DialogDescription>
            <span className="mb-1 block font-medium text-foreground">{provider.name}</span>
            {msg(`platformAdmin.oauthManagement.${action}Body`, actionBodies[action])}
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <DialogFooter>
          <DialogClose render={<Button type="button" variant="outline" disabled={mutation.isPending} />}>
            {msg('common.cancel', 'Cancel')}
          </DialogClose>
          <Button type="button" disabled={mutation.isPending} onClick={() => mutation.mutate()}>
            {mutation.isPending ? msg('common.saving', 'Saving...') : msg('common.confirm', 'Confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
