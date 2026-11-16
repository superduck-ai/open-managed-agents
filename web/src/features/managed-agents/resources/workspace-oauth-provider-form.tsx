import { type ComponentProps, useEffect, useRef, useState } from 'react';
import { ChevronLeft } from 'lucide-react';
import { useMutation, useQuery } from '@tanstack/react-query';
import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { useAuth } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Field, FieldLabel, FieldDescription } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { usePlatformAdminError } from '../../platform-admin/usePlatformAdminError';
import { createWorkspaceOAuthProvider, getWorkspaceOAuthProvider, updateWorkspaceOAuthProvider } from '../api';
import {
  initialOAuthProviderFormValues,
  oauthProviderOptionalFields,
} from '../../platform-admin/oauth/oauthProviderForm';
import { OptionalCredentialFields } from './optional-credential-fields';

export function WorkspaceOAuthProviderForm({
  orgUuid,
  workspaceId,
  onClose,
  onSaved,
  provider,
  onPendingChange,
}: {
  orgUuid: string;
  workspaceId: string;
  onPendingChange: (pending: boolean) => void;
  onClose: () => void;
  onSaved: (provider: OAuthProviderRecord) => void;
  provider?: OAuthProviderRecord;
}) {
  const { msg } = useI18n();
  const { csrfToken } = useAuth();
  const errorMessage = usePlatformAdminError();
  const formRef = useRef<HTMLFormElement>(null);
  const [clientOpen, setClientOpen] = useState(Boolean(provider));
  const [clientError, setClientError] = useState<'clientId' | 'clientSecret' | null>(null);
  useEffect(() => {
    if (clientError && clientOpen) formRef.current?.querySelector<HTMLInputElement>('[aria-invalid="true"]')?.focus();
  }, [clientError, clientOpen]);
  const { name, authorizeEndpoint, tokenEndpoint, scope, clientId } = initialOAuthProviderFormValues(provider);
  const [values, setValues] = useState({
    name,
    authorizeEndpoint,
    tokenEndpoint,
    scope,
    clientId,
    clientSecret: '',
  });
  const patch = (next: Partial<typeof values>) => setValues((current) => ({ ...current, ...next }));
  const mutation = useMutation({
    mutationFn: () => {
      const input = {
        name: values.name.trim(),
        authorize_endpoint: values.authorizeEndpoint.trim(),
        token_endpoint: values.tokenEndpoint.trim(),
        scope: values.scope.trim(),
        ...(values.clientId.trim() ? { client_id: values.clientId.trim() } : {}),
        ...(values.clientSecret.trim() ? { client_secret: values.clientSecret.trim() } : {}),
      };
      return provider
        ? updateWorkspaceOAuthProvider(
            orgUuid,
            workspaceId,
            provider.id,
            { ...input, expected_version: provider.version },
            csrfToken,
          )
        : createWorkspaceOAuthProvider(orgUuid, workspaceId, input, csrfToken);
    },
    onMutate: () => onPendingChange(true),
    onSettled: () => onPendingChange(false),
    onSuccess: onSaved,
  });
  const optional = oauthProviderOptionalFields(values, provider);
  const ready = [values.name, values.authorizeEndpoint, values.tokenEndpoint].every((value) => value.trim());
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
  const title = provider
    ? msg('managedAgents.credentialVaults.credentialDialog.editProvider', 'Edit OAuth provider')
    : msg('managedAgents.credentialVaults.credentialDialog.customProvider', 'Custom OAuth provider');
  return (
    <form
      ref={formRef}
      aria-label={title}
      className="space-y-4 p-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (!ready || mutation.isPending) return;
        if (
          (!optional.clientId && !values.clientId.trim()) ||
          (!optional.clientSecret && !values.clientSecret.trim())
        ) {
          formRef.current?.querySelector<HTMLInputElement>('[aria-invalid="true"]')?.focus();
          setClientError(values.clientId.trim() ? 'clientSecret' : 'clientId');
          setClientOpen(true);
          return;
        }
        mutation.mutate();
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
        {title}
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
              autoFocus={field.key === 'name'}
              placeholder={field.placeholder}
              value={values[field.key]}
              onChange={(event) => patch({ [field.key]: event.target.value })}
            />
          </Field>
        ))}
      </div>
      <FieldDescription>
        {msg(
          'managedAgents.credentialVaults.credentialDialog.clientConfigurationHint',
          'Authorization requires client ID and secret. Leave both blank only when using a token supplied manually.',
        )}
      </FieldDescription>
      <OptionalCredentialFields
        title={msg('managedAgents.credentialVaults.credentialDialog.oauthClientConfiguration', 'Client configuration')}
        open={clientOpen}
        onOpenChange={setClientOpen}
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="workspace-oauth-client-id">
              {msg('platformAdmin.oauthManagement.clientId', 'Client ID')}
            </FieldLabel>
            <Input
              id="workspace-oauth-client-id"
              aria-invalid={clientError === 'clientId' || undefined}
              aria-describedby={clientError === 'clientId' ? 'workspace-oauth-client-error' : undefined}
              value={values.clientId}
              onChange={(event) => {
                patch({ clientId: event.target.value });
                setClientError(null);
              }}
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="workspace-oauth-client-secret">
              {msg('platformAdmin.oauthManagement.clientSecret', 'Client secret')}
            </FieldLabel>
            <Input
              id="workspace-oauth-client-secret"
              type="password"
              placeholder={
                provider?.has_client_secret && values.clientId.trim() === provider.client_id
                  ? msg('platformAdmin.oauthManagement.secretKeepHint', 'Leave blank to keep the current secret.')
                  : undefined
              }
              aria-invalid={clientError === 'clientSecret' || undefined}
              aria-describedby={clientError === 'clientSecret' ? 'workspace-oauth-client-error' : undefined}
              value={values.clientSecret}
              onChange={(event) => {
                patch({ clientSecret: event.target.value });
                setClientError(null);
              }}
            />
          </Field>
        </div>
      </OptionalCredentialFields>
      {clientError ? (
        <p id="workspace-oauth-client-error" role="alert" className="text-sm text-destructive">
          {msg(
            'platformAdmin.oauthManagement.firstConfiguration',
            'Enter both client ID and client secret to configure this provider.',
          )}
        </p>
      ) : null}
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
          {mutation.isPending
            ? msg('common.saving', 'Saving...')
            : provider
              ? msg('common.save', 'Save')
              : msg('common.add', 'Add')}
        </Button>
      </div>
    </form>
  );
}

export function WorkspaceOAuthProviderEditor({
  providerId,
  ...props
}: Omit<ComponentProps<typeof WorkspaceOAuthProviderForm>, 'provider'> & { providerId?: string }) {
  const { msg } = useI18n();
  const errorMessage = usePlatformAdminError();
  const query = useQuery({
    queryKey: ['workspace-oauth-provider', props.orgUuid, props.workspaceId, providerId],
    queryFn: () => getWorkspaceOAuthProvider(props.orgUuid, props.workspaceId, providerId!),
    enabled: Boolean(providerId),
    refetchOnWindowFocus: false,
    staleTime: Infinity,
    gcTime: 0,
  });
  if (providerId && !query.data)
    return (
      <div className="space-y-3 p-4">
        <Button type="button" variant="ghost" onClick={props.onClose}>
          <ChevronLeft className="size-4" aria-hidden />
          {msg('managedAgents.credentialVaults.credentialDialog.editProvider', 'Edit OAuth provider')}
        </Button>
        {query.error ? (
          <>
            <p role="alert" className="text-sm text-destructive">
              {errorMessage(query.error)}
            </p>
            <Button type="button" variant="outline" onClick={() => void query.refetch()}>
              {msg('common.retry', 'Retry')}
            </Button>
          </>
        ) : (
          <p>{msg('common.loading', 'Loading...')}</p>
        )}
      </div>
    );
  return <WorkspaceOAuthProviderForm {...props} provider={query.data} />;
}
