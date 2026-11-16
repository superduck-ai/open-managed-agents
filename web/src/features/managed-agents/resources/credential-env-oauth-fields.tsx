import type { Ref } from 'react';
import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';
import { EnvOAuthClientFields, type EnvOAuthClientFieldsHandle } from './env-oauth-client-fields';
import { WorkspaceOAuthProviderPicker } from './workspace-oauth-provider-picker';
import { useI18n } from '../../../shared/i18n';
import { OAuthProviderAvatar } from '../../../shared/icons/oauth-provider-avatar';
import { Button } from '../../../shared/ui/button';
import { FieldDescription } from '../../../shared/ui/field';
import { ManagedTextField } from '../components/common';
import { type CredentialFormValues, type EnvOAuthProviderApiResponse } from '../types';
import { CredentialNetworkingFields } from './credential-networking-fields';
import { OptionalCredentialFields } from './optional-credential-fields';
import { credentialEnvHostsMissing, envOAuthProviderDefaults } from './model';

type EnvOAuthCredentialFieldsProps = {
  orgUuid?: string;
  workspaceId?: string;
  clientFieldsRef?: Ref<EnvOAuthClientFieldsHandle>;
  onProviderSaved?: (provider: EnvOAuthProviderApiResponse) => void;
  values: CredentialFormValues;
  providers: EnvOAuthProviderApiResponse[];
  providersLoading: boolean;
  providersError: string | null;
  onRetryProviders: () => void;
  secretNameLocked: boolean;
  onChange: (patch: Partial<CredentialFormValues>) => void;
};

export function EnvOAuthCredentialFields({
  orgUuid,
  workspaceId,
  onProviderSaved,
  clientFieldsRef,
  values,
  providers,
  providersLoading,
  providersError,
  onRetryProviders,
  secretNameLocked,
  onChange,
}: EnvOAuthCredentialFieldsProps) {
  const { msg } = useI18n();
  const selected = providers.find((provider) => provider.id === values.envOAuthProviderId);
  const hostsMissing = credentialEnvHostsMissing(values);
  const options = providers
    .filter((provider) => provider.enabled || (secretNameLocked && provider.id === values.envOAuthProviderId))
    .map((provider) => ({
      id: provider.id,
      label: provider.workspace_scoped
        ? `${provider.name} (${msg('settings.scope.workspace', 'Workspace')})`
        : provider.name,
      icon: <OAuthProviderAvatar logoUrl={provider.logo_url} />,
    }));
  const selectableOptions =
    secretNameLocked && selected ? options.filter((option) => option.id === selected.id) : options;
  const providerSaved = (record: OAuthProviderRecord) => {
    const provider = { ...record, id: record.provider_key, workspace_scoped: true };
    onProviderSaved?.(provider);
    if (provider.id === values.envOAuthProviderId) return;
    const defaults = envOAuthProviderDefaults(provider);
    const previousDefault = selected ? envOAuthProviderDefaults(selected).secretName : '';
    onChange({
      envOAuthProviderId: provider.id,
      allowedHostsText: defaults.allowedHostsText,
      ...(values.secretName.trim() && values.secretName !== previousDefault ? {} : { secretName: defaults.secretName }),
    });
  };
  return (
    <div className="space-y-4">
      <WorkspaceOAuthProviderPicker
        label={msg('managedAgents.credentialVaults.credentialDialog.envOAuthProvider', 'OAuth provider')}
        value={values.envOAuthProviderId}
        placeholder={
          providersLoading
            ? msg('managedAgents.credentialVaults.credentialDialog.envOAuthProvidersLoading', 'Loading providers...')
            : msg('managedAgents.credentialVaults.credentialDialog.envOAuthSelectProvider', 'Select a provider')
        }
        options={selectableOptions}
        orgUuid={orgUuid}
        canCreate={!secretNameLocked}
        editableProviderId={selected?.workspace_scoped ? selected.id.slice('custom:'.length) : undefined}
        workspaceId={workspaceId}
        onChange={(providerId) => {
          if (secretNameLocked || providerId === values.envOAuthProviderId) {
            return;
          }
          const provider = providers.find((item) => item.id === providerId);
          const defaults = provider ? envOAuthProviderDefaults(provider) : null;
          const previousDefault = selected ? envOAuthProviderDefaults(selected).secretName : '';
          const secretNameEdited = values.secretName.trim() !== '' && values.secretName !== previousDefault;
          onChange({
            envOAuthProviderId: providerId,
            ...(defaults
              ? {
                  allowedHostsText: defaults.allowedHostsText,
                  ...(secretNameEdited ? {} : { secretName: defaults.secretName }),
                }
              : {}),
          });
        }}
        onSaved={providerSaved}
      />
      {providersError ? (
        <div className="flex flex-wrap items-center gap-2">
          <FieldDescription className="text-destructive">{providersError}</FieldDescription>
          <Button type="button" variant="outline" size="sm" onClick={onRetryProviders}>
            {msg('managedAgents.credentialVaults.credentialDialog.envOAuthProvidersRetry', 'Retry')}
          </Button>
        </div>
      ) : null}
      {selected && !selected.configured && !selected.workspace_scoped ? (
        <FieldDescription>
          {msg(
            'managedAgents.credentialVaults.credentialDialog.envOAuthUnconfigured',
            'Authorization requires client ID and secret. Configure this provider before connecting, or supply a token manually.',
          )}
        </FieldDescription>
      ) : null}
      <ManagedTextField
        label={msg('managedAgents.credentialVaults.credentialDialog.variableName', 'Variable name')}
        value={values.secretName}
        placeholder={msg(
          'managedAgents.credentialVaults.credentialDialog.envOAuthSecretNamePlaceholder',
          'ACCESS_TOKEN',
        )}
        disabled={secretNameLocked}
        onChange={(secretName) => onChange({ secretName })}
      />
      <CredentialNetworkingFields
        networkType={values.networkType}
        allowedHostsText={values.allowedHostsText}
        hostsMissing={hostsMissing}
        onChange={onChange}
      />
      <OptionalCredentialFields
        title={msg('managedAgents.credentialVaults.credentialDialog.envOAuthManual', 'Use an existing token')}
        defaultOpen={Boolean(values.envOAuthManualToken)}
      >
        <ManagedTextField
          label={msg('managedAgents.credentialVaults.credentialDialog.envOAuthManualToken', 'Access token')}
          type="password"
          value={values.envOAuthManualToken}
          placeholder={
            secretNameLocked
              ? msg(
                  'managedAgents.credentialVaults.credentialDialog.keepToken',
                  'Leave blank to keep the current token',
                )
              : msg(
                  'managedAgents.credentialVaults.credentialDialog.envOAuthManualTokenPlaceholder',
                  'Paste a token to skip authorization',
                )
          }
          onChange={(envOAuthManualToken) => onChange({ envOAuthManualToken })}
        />
      </OptionalCredentialFields>
      {selected?.workspace_scoped && orgUuid && workspaceId ? (
        <EnvOAuthClientFields
          key={selected.id}
          orgUuid={orgUuid}
          workspaceId={workspaceId}
          providerId={selected.id.slice('custom:'.length)}
          clientRef={clientFieldsRef}
          manualToken={Boolean(values.envOAuthManualToken.trim())}
          onSaved={providerSaved}
        />
      ) : null}
      <FieldDescription>
        {msg(
          'managedAgents.credentialVaults.credentialDialog.envOAuthHint',
          'The access token is injected as an Authorization header for the allowed hosts and refreshed automatically when it expires.',
        )}
      </FieldDescription>
    </div>
  );
}
