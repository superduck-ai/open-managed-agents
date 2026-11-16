import { useState } from 'react';
import { ChevronLeft } from 'lucide-react';
import { useMutation } from '@tanstack/react-query';
import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { useAuth } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { usePlatformAdminError } from '../../platform-admin/usePlatformAdminError';
import { createWorkspaceOAuthProvider } from '../api';
import { OptionalCredentialFields } from './optional-credential-fields';

export function WorkspaceOAuthProviderForm({
  orgUuid,
  workspaceId,
  onClose,
  onCreated,
  onPendingChange,
}: {
  orgUuid: string;
  workspaceId: string;
  onPendingChange: (pending: boolean) => void;
  onClose: () => void;
  onCreated: (provider: OAuthProviderRecord) => void;
}) {
  const { msg } = useI18n();
  const { csrfToken } = useAuth();
  const errorMessage = usePlatformAdminError();
  const [values, setValues] = useState({
    name: '',
    authorizeEndpoint: '',
    tokenEndpoint: '',
    scope: '',
    clientId: '',
    clientSecret: '',
  });
  const patch = (next: Partial<typeof values>) => setValues((current) => ({ ...current, ...next }));
  const mutation = useMutation({
    mutationFn: () =>
      createWorkspaceOAuthProvider(
        orgUuid,
        workspaceId,
        {
          name: values.name.trim(),
          authorize_endpoint: values.authorizeEndpoint.trim(),
          token_endpoint: values.tokenEndpoint.trim(),
          scope: values.scope.trim(),
          ...(values.clientId.trim()
            ? { client_id: values.clientId.trim(), client_secret: values.clientSecret.trim() }
            : {}),
        },
        csrfToken,
      ),
    onMutate: () => onPendingChange(true),
    onSettled: () => onPendingChange(false),
    onSuccess: onCreated,
  });
  const ready =
    [values.name, values.authorizeEndpoint, values.tokenEndpoint].every((value) => value.trim()) &&
    Boolean(values.clientId.trim()) === Boolean(values.clientSecret.trim());
  const fields = [
    {
      key: 'name',
      label: msg('platformAdmin.oauthManagement.name', 'Name'),
      placeholder: msg('managedAgents.common.namePlaceholder', 'Enter a name'),
    },
    {
      key: 'authorizeEndpoint',
      label: msg('platformAdmin.oauthManagement.authorizeEndpoint', 'Authorize endpoint'),
      placeholder: 'https://example.com/oauth/authorize',
    },
    {
      key: 'tokenEndpoint',
      label: msg('platformAdmin.oauthManagement.tokenEndpoint', 'Token endpoint'),
      placeholder: 'https://example.com/oauth/token',
    },
    {
      key: 'scope',
      label: msg('platformAdmin.oauthManagement.scope', 'Scope'),
      placeholder: 'read write offline_access',
    },
  ] as const;
  return (
    <form
      aria-label={msg('managedAgents.credentialVaults.credentialDialog.customProvider', 'Custom OAuth provider')}
      className="space-y-4 p-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (ready && !mutation.isPending) mutation.mutate();
      }}
    >
      <Button
        type="button"
        variant="ghost"
        className="w-full justify-start border-b rounded-none pb-4"
        disabled={mutation.isPending}
        onClick={onClose}
      >
        <ChevronLeft className="size-4" aria-hidden />
        {msg('managedAgents.credentialVaults.credentialDialog.customProvider', 'Custom OAuth provider')}
      </Button>
      <div className="grid gap-4 sm:grid-cols-2">
        {fields.map((field) => (
          <Field key={field.key}>
            <FieldLabel htmlFor={`workspace-oauth-${field.key}`}>
              {field.label}
              {field.key === 'scope' ? (
                <span className="font-normal text-muted-foreground">
                  {msg('managedAgents.common.optionalParen', '(optional)')}
                </span>
              ) : null}
            </FieldLabel>
            <Input
              id={`workspace-oauth-${field.key}`}
              placeholder={field.placeholder}
              value={values[field.key]}
              onChange={(event) => patch({ [field.key]: event.target.value })}
            />
          </Field>
        ))}
      </div>
      <OptionalCredentialFields
        title={msg('managedAgents.credentialVaults.credentialDialog.oauthClientConfiguration', 'Client configuration')}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="workspace-oauth-client-id">
              {msg('platformAdmin.oauthManagement.clientId', 'Client ID')}
            </FieldLabel>
            <Input
              id="workspace-oauth-client-id"
              value={values.clientId}
              onChange={(event) => patch({ clientId: event.target.value })}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="workspace-oauth-client-secret">
              {msg('platformAdmin.oauthManagement.clientSecret', 'Client secret')}
            </FieldLabel>
            <Input
              id="workspace-oauth-client-secret"
              type="password"
              value={values.clientSecret}
              onChange={(event) => patch({ clientSecret: event.target.value })}
            />
          </Field>
        </div>
      </OptionalCredentialFields>
      {mutation.error ? (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(mutation.error)}
        </p>
      ) : null}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" disabled={mutation.isPending} onClick={onClose}>
          {msg('common.cancel', 'Cancel')}
        </Button>
        <Button type="submit" disabled={!ready || mutation.isPending}>
          {mutation.isPending ? msg('common.saving', 'Saving...') : msg('common.add', 'Add')}
        </Button>
      </div>
    </form>
  );
}
