import { type Ref, useEffect, useId, useImperativeHandle, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import type { ApiError } from '../../../shared/api/client';
import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { useAuth } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Field, FieldDescription, FieldLabel } from '../../../shared/ui/field';
import { Input } from '../../../shared/ui/input';
import { usePlatformAdminError } from '../../platform-admin/usePlatformAdminError';
import { getWorkspaceOAuthProvider, updateWorkspaceOAuthProvider } from '../api';
import { OptionalCredentialFields } from './optional-credential-fields';

export type EnvOAuthClientFieldsHandle = {
  validate: () => boolean;
  save: () => Promise<void>;
};

export function EnvOAuthClientFields({
  orgUuid,
  workspaceId,
  providerId,
  clientRef,
  manualToken,
  onSaved,
}: {
  orgUuid: string;
  workspaceId: string;
  providerId: string;
  clientRef?: Ref<EnvOAuthClientFieldsHandle>;
  manualToken: boolean;
  onSaved: (provider: OAuthProviderRecord) => void;
}) {
  const { msg } = useI18n();
  const id = useId();
  const { csrfToken } = useAuth();
  const errorMessage = usePlatformAdminError();
  const queryClient = useQueryClient();
  const queryKey = ['workspace-oauth-provider', orgUuid, workspaceId, providerId];
  const query = useQuery({
    queryKey,
    queryFn: () => getWorkspaceOAuthProvider(orgUuid, workspaceId, providerId),
    refetchOnWindowFocus: false,
    staleTime: Infinity,
    gcTime: 0,
  });
  const [editedClientId, setClientId] = useState<string | null>(null);
  const clientId = editedClientId ?? query.data?.client_id ?? '';
  const [clientSecret, setClientSecret] = useState('');
  const [open, setOpen] = useState(false);
  const [attempted, setAttempted] = useState(false);
  const fieldsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (manualToken) setAttempted(false);
  }, [manualToken]);
  useEffect(() => {
    if (attempted && open) fieldsRef.current?.querySelector<HTMLInputElement>('[aria-invalid="true"]')?.focus();
  }, [attempted, open, query.isSuccess]);
  const missingId = !clientId.trim();
  const missingSecret =
    !clientSecret.trim() && (!query.data?.has_client_secret || clientId.trim() !== query.data.client_id);
  useImperativeHandle(clientRef, () => ({
    validate() {
      setAttempted(true);
      if (!query.data || missingId || missingSecret) {
        setOpen(true);
        fieldsRef.current?.querySelector<HTMLInputElement>('[aria-invalid="true"]')?.focus();
        return false;
      }
      return true;
    },
    async save() {
      const record = query.data!;
      if (clientId.trim() === record.client_id && !clientSecret.trim()) return;
      try {
        const saved = await updateWorkspaceOAuthProvider(
          orgUuid,
          workspaceId,
          providerId,
          {
            expected_version: record.version,
            client_id: clientId.trim(),
            ...(clientSecret.trim() ? { client_secret: clientSecret.trim() } : {}),
          },
          csrfToken,
        );
        queryClient.setQueryData(queryKey, saved);
        setClientId(null);
        setClientSecret('');
        onSaved(saved);
      } catch (error) {
        if ((error as ApiError).code === 'oauth_provider_version_conflict') {
          setClientId(clientId);
          await query.refetch();
        }
        throw error;
      }
    },
  }));
  const fields = [
    {
      id: `${id}-client-id`,
      label: msg('platformAdmin.oauthManagement.clientId', 'Client ID'),
      type: 'text',
      value: clientId,
      setValue: setClientId,
      invalid: attempted && missingId,
      placeholder: undefined,
      error: msg(
        'managedAgents.credentialVaults.credentialDialog.clientIdRequired',
        'Custom OAuth authorization requires a client ID.',
      ),
    },
    {
      id: `${id}-client-secret`,
      label: msg('platformAdmin.oauthManagement.clientSecret', 'Client secret'),
      type: 'password',
      value: clientSecret,
      setValue: (value: string) => {
        setClientId(clientId);
        setClientSecret(value);
      },
      invalid: attempted && missingSecret,
      placeholder:
        query.data?.has_client_secret && clientId.trim() === query.data.client_id
          ? msg('platformAdmin.oauthManagement.secretKeepHint', 'Leave blank to keep the current secret.')
          : undefined,
      error: msg(
        'managedAgents.credentialVaults.credentialDialog.clientSecretRequired',
        'Custom OAuth authorization requires a client secret.',
      ),
    },
  ];
  return (
    <OptionalCredentialFields
      title={msg('managedAgents.credentialVaults.credentialDialog.oauthClientConfiguration', 'Client configuration')}
      required={!manualToken || attempted}
      open={open}
      onOpenChange={setOpen}
    >
      <div ref={fieldsRef} className="grid gap-4 sm:grid-cols-2">
        {fields.map((field) => (
          <Field key={field.id}>
            <FieldLabel htmlFor={field.id}>{field.label}</FieldLabel>
            <Input
              id={field.id}
              type={field.type}
              value={field.value}
              disabled={!query.data}
              onChange={(event) => field.setValue(event.target.value)}
              placeholder={field.placeholder}
              aria-invalid={field.invalid || undefined}
              aria-describedby={field.invalid ? `${field.id}-error` : undefined}
            />
            {field.invalid ? (
              <FieldDescription id={`${field.id}-error`} role="alert" className="text-destructive">
                {field.error}
              </FieldDescription>
            ) : null}
          </Field>
        ))}
      </div>
      {query.error ? (
        <>
          <p role="alert" className="text-sm text-destructive">
            {errorMessage(query.error)}
          </p>
          <Button type="button" variant="outline" onClick={() => void query.refetch()}>
            {msg('common.retry', 'Retry')}
          </Button>
        </>
      ) : !query.data ? (
        <p>{msg('common.loading', 'Loading...')}</p>
      ) : null}
    </OptionalCredentialFields>
  );
}
