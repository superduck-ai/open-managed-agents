import { useState, type ReactNode } from 'react';
import { useMutation } from '@tanstack/react-query';
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
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { createOAuthProvider, updateOAuthProvider, type OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { usePlatformAdmin } from '../context';
import { usePlatformAdminError } from '../usePlatformAdminError';

type OAuthProviderFormValues = {
  name: string;
  authorizeEndpoint: string;
  tokenEndpoint: string;
  scope: string;
  clientId: string;
  clientSecret: string;
  authMethod: string;
  encoding: string;
};

function initialValues(provider?: OAuthProviderRecord): OAuthProviderFormValues {
  return {
    name: provider?.name ?? '',
    authorizeEndpoint: provider?.authorize_endpoint ?? '',
    tokenEndpoint: provider?.token_endpoint ?? '',
    scope: provider?.scope ?? '',
    clientId: provider?.client_id ?? '',
    clientSecret: '',
    authMethod: provider?.token_endpoint_auth_method ?? 'client_secret_post',
    encoding: provider?.token_endpoint_encoding ?? 'form',
  };
}

export function OAuthProviderDialog({
  provider,
  onClose,
  onSaved,
}: {
  provider?: OAuthProviderRecord;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const { msg } = useI18n();
  const { client } = usePlatformAdmin();
  const errorMessage = usePlatformAdminError();
  const [values, setValues] = useState<OAuthProviderFormValues>(() => initialValues(provider));
  const [error, setError] = useState<string | null>(null);
  const patch = (next: Partial<OAuthProviderFormValues>) => setValues((current) => ({ ...current, ...next }));
  const mutation = useMutation({
    mutationFn: () => {
      const input = {
        name: values.name.trim(),
        authorize_endpoint: values.authorizeEndpoint.trim(),
        token_endpoint: values.tokenEndpoint.trim(),
        scope: values.scope.trim(),
        client_id: values.clientId.trim(),
        token_endpoint_auth_method: values.authMethod,
        token_endpoint_encoding: values.encoding,
        ...(values.clientSecret.trim() ? { client_secret: values.clientSecret.trim() } : {}),
      };
      return provider
        ? updateOAuthProvider(client, provider.id, input)
        : createOAuthProvider(client, { ...input, client_secret: values.clientSecret.trim() });
    },
    onSuccess: () => {
      void onSaved();
    },
    onError: (submitError) => setError(errorMessage(submitError)),
  });
  const canSubmit =
    values.name.trim() &&
    values.authorizeEndpoint.trim() &&
    values.tokenEndpoint.trim() &&
    values.clientId.trim() &&
    (provider ? true : values.clientSecret.trim());
  return (
    <Dialog open onOpenChange={(open) => !open && !mutation.isPending && onClose()}>
      <DialogContent className="sm:max-w-2xl" closeButtonDisabled={mutation.isPending}>
        <DialogHeader>
          <DialogTitle>
            {provider
              ? msg('platformAdmin.oauthManagement.editTitle', 'Edit OAuth client')
              : msg('platformAdmin.oauthManagement.createTitle', 'Register OAuth client')}
          </DialogTitle>
          <DialogDescription>
            {msg(
              'platformAdmin.oauthManagement.dialogDescription',
              'Credentials are encrypted at rest and never returned by the API.',
            )}
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (canSubmit && !mutation.isPending) {
              setError(null);
              mutation.mutate();
            }
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field id="oauth-provider-name" label={msg('platformAdmin.oauthManagement.name', 'Name')}>
              <Input
                id="oauth-provider-name"
                value={values.name}
                onChange={(event) => patch({ name: event.target.value })}
              />
            </Field>
            <Field id="oauth-provider-client-id" label={msg('platformAdmin.oauthManagement.clientId', 'Client ID')}>
              <Input
                id="oauth-provider-client-id"
                value={values.clientId}
                onChange={(event) => patch({ clientId: event.target.value })}
              />
            </Field>
            <Field
              id="oauth-provider-authorize"
              label={msg('platformAdmin.oauthManagement.authorizeEndpoint', 'Authorize endpoint')}
              className="sm:col-span-2"
            >
              <Input
                id="oauth-provider-authorize"
                value={values.authorizeEndpoint}
                placeholder="https://example.com/oauth/authorize"
                onChange={(event) => patch({ authorizeEndpoint: event.target.value })}
              />
            </Field>
            <Field
              id="oauth-provider-token"
              label={msg('platformAdmin.oauthManagement.tokenEndpoint', 'Token endpoint')}
              className="sm:col-span-2"
            >
              <Input
                id="oauth-provider-token"
                value={values.tokenEndpoint}
                placeholder="https://example.com/oauth/token"
                onChange={(event) => patch({ tokenEndpoint: event.target.value })}
              />
            </Field>
            <Field id="oauth-provider-scope" label={msg('platformAdmin.oauthManagement.scope', 'Scope')}>
              <Input
                id="oauth-provider-scope"
                value={values.scope}
                onChange={(event) => patch({ scope: event.target.value })}
              />
            </Field>
            <Field
              id="oauth-provider-secret"
              label={msg('platformAdmin.oauthManagement.clientSecret', 'Client secret')}
              hint={
                provider
                  ? msg('platformAdmin.oauthManagement.secretKeepHint', 'Leave blank to keep the current secret.')
                  : undefined
              }
            >
              <Input
                id="oauth-provider-secret"
                type="password"
                value={values.clientSecret}
                onChange={(event) => patch({ clientSecret: event.target.value })}
              />
            </Field>
            <Field
              id="oauth-provider-auth-method"
              label={msg('platformAdmin.oauthManagement.authMethod', 'Token endpoint auth')}
            >
              <Select value={values.authMethod} onValueChange={(value) => value && patch({ authMethod: value })}>
                <SelectTrigger id="oauth-provider-auth-method">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="client_secret_post">client_secret_post</SelectItem>
                  <SelectItem value="client_secret_basic">client_secret_basic</SelectItem>
                  <SelectItem value="none">none</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field id="oauth-provider-encoding" label={msg('platformAdmin.oauthManagement.encoding', 'Token encoding')}>
              <Select value={values.encoding} onValueChange={(value) => value && patch({ encoding: value })}>
                <SelectTrigger id="oauth-provider-encoding">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="form">form</SelectItem>
                  <SelectItem value="json">json</SelectItem>
                </SelectContent>
              </Select>
            </Field>
          </div>
          {error ? <p className="text-sm text-destructive">{error}</p> : null}
          <DialogFooter>
            <DialogClose render={<Button type="button" variant="outline" disabled={mutation.isPending} />}>
              {msg('common.cancel', 'Cancel')}
            </DialogClose>
            <Button type="submit" disabled={!canSubmit || mutation.isPending}>
              {mutation.isPending ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Field({
  id,
  label,
  hint,
  className,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div className={className}>
      <Label htmlFor={id} className="text-sm font-medium text-foreground">
        {label}
      </Label>
      <div className="mt-2">{children}</div>
      {hint ? <p className="mt-1 text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  );
}
