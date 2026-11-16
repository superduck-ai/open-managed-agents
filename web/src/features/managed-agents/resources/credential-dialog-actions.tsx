import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { DialogFooter } from '../../../shared/ui/dialog';

type CredentialDialogActionsProps = {
  firstCredential: boolean;
  editing: boolean;
  envOAuth: boolean;
  envOAuthConfigured: boolean;
  needsOAuthConnect: boolean;
  canSubmit: boolean;
  submitting: boolean;
  waitingForOAuth: boolean;
  onDismiss: () => void;
  onConnect: (reauthorize?: boolean) => Promise<void>;
};

export function CredentialDialogActions({
  firstCredential,
  editing,
  envOAuth,
  envOAuthConfigured,
  needsOAuthConnect,
  canSubmit,
  submitting,
  waitingForOAuth,
  onDismiss,
  onConnect,
}: CredentialDialogActionsProps) {
  const { msg } = useI18n();
  return (
    <DialogFooter className="mt-5 shrink-0">
      <Button
        type="button"
        variant={firstCredential ? 'ghost' : 'outline'}
        disabled={submitting && !waitingForOAuth}
        onClick={onDismiss}
      >
        {firstCredential ? msg('managedAgents.quickstart.skip', 'Skip') : msg('common.cancel', 'Cancel')}
      </Button>
      {editing && envOAuth ? (
        <Button
          type="button"
          variant="outline"
          disabled={!canSubmit || submitting || !envOAuthConfigured}
          onClick={() => void onConnect(true)}
        >
          {waitingForOAuth
            ? msg('managedAgents.credentialVaults.credentialDialog.connecting', 'Connecting...')
            : msg('managedAgents.credentialVaults.credentialDialog.reauthorize', 'Reauthorize')}
        </Button>
      ) : null}
      {needsOAuthConnect ? (
        <Button
          type="button"
          disabled={!canSubmit || submitting || (envOAuth && !envOAuthConfigured)}
          onClick={() => void onConnect()}
        >
          {submitting
            ? msg('managedAgents.credentialVaults.credentialDialog.connecting', 'Connecting...')
            : msg('managedAgents.credentialVaults.credentialDialog.connect', 'Connect')}
        </Button>
      ) : (
        <Button type="submit" disabled={!canSubmit || submitting}>
          {submitting && !waitingForOAuth
            ? msg('common.saving', 'Saving...')
            : editing
              ? msg('common.saveChanges', 'Save changes')
              : msg('managedAgents.credentialVaults.credentialDialog.add', 'Add credential')}
        </Button>
      )}
    </DialogFooter>
  );
}
