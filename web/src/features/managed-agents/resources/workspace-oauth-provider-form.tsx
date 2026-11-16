import { type ComponentProps, useState } from 'react';
import { ChevronLeft } from 'lucide-react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { ApiError } from '../../../shared/api/client';
import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { useAuth } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Field, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { usePlatformAdminError } from '../../platform-admin/usePlatformAdminError';
import { createWorkspaceOAuthProvider, getWorkspaceOAuthProvider, updateWorkspaceOAuthProvider } from '../api';
import { initialOAuthProviderFormValues } from '../../platform-admin/oauth/oauthProviderForm';

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
  const queryClient = useQueryClient();
  const { name, authorizeEndpoint, tokenEndpoint, scope } = initialOAuthProviderFormValues(provider);
  const [values, setValues] = useState({ name, authorizeEndpoint, tokenEndpoint, scope });
  const patch = (next: Partial<typeof values>) => setValues((current) => ({ ...current, ...next }));
  const mutation = useMutation<OAuthProviderRecord, ApiError>({
    mutationFn: () => {
      const input = {
        name: values.name.trim(),
        authorize_endpoint: values.authorizeEndpoint.trim(),
        token_endpoint: values.tokenEndpoint.trim(),
        scope: values.scope.trim(),
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
    onError: async (error) => {
      if (error.code === 'oauth_provider_version_conflict' && provider) {
        await queryClient.refetchQueries({ queryKey: ['workspace-oauth-provider', orgUuid, workspaceId, provider.id] });
      }
    },
  });
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
      aria-label={title}
      className="space-y-4 p-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (!ready || mutation.isPending) return;
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
      {mutation.error ? (
        <p role="alert" className="text-sm text-destructive">
          {mutation.error.code === 'oauth_provider_version_conflict'
            ? msg(
                'managedAgents.credentialVaults.credentialDialog.providerVersionConflict',
                'The provider configuration changed. Your input is preserved; review it and try again.',
              )
            : errorMessage(mutation.error)}
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
  const queryClient = useQueryClient();
  const queryKey = ['workspace-oauth-provider', props.orgUuid, props.workspaceId, providerId];
  const query = useQuery({
    queryKey,
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
  return (
    <WorkspaceOAuthProviderForm
      {...props}
      provider={query.data}
      onSaved={(provider) => {
        if (providerId) queryClient.setQueryData(queryKey, provider);
        props.onSaved(provider);
      }}
    />
  );
}
