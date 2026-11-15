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

export function OAuthProviderStatusDialog({
  provider,
  onClose,
  onSaved,
  onConflict,
}: {
  provider: OAuthProviderRecord;
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
      updateOAuthProvider(client, provider.id, { enabled: !provider.enabled, expected_version: provider.version }),
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
          <DialogTitle>
            {provider.enabled
              ? msg('platformAdmin.oauthManagement.disableTitle', 'Disable OAuth client?')
              : msg('platformAdmin.oauthManagement.enableTitle', 'Enable OAuth client?')}
          </DialogTitle>
          <DialogDescription>
            <span className="mb-1 block font-medium text-foreground">{provider.name}</span>
            {provider.enabled
              ? msg(
                  'platformAdmin.oauthManagement.disableBody',
                  'Hide from the workspace provider catalog. Existing credentials and sessions remain available; disabling does not revoke authorization or automatic refresh.',
                )
              : msg(
                  'platformAdmin.oauthManagement.enableBody',
                  'Show in the workspace provider catalog for creating new credentials.',
                )}
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
