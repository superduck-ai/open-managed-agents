import type { Ref } from 'react';
import type { CustomOAuthProvider } from '../types';
import { EnvOAuthClientFields, type EnvOAuthClientFieldsHandle } from './env-oauth-client-fields';
import { CustomOAuthProviderPicker } from './custom-oauth-provider-picker';
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
  clientFieldsRef?: Ref<EnvOAuthClientFieldsHandle>;
  values: CredentialFormValues;
  providers: EnvOAuthProviderApiResponse[];
  providersLoading: boolean;
  providersError: string | null;
  onRetryProviders: () => void;
  secretNameLocked: boolean;
  onChange: (patch: Partial<CredentialFormValues>) => void;
};

export function EnvOAuthCredentialFields({
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
  const custom = values.envOAuthCustomProvider;
  const availableProviders = custom
    ? [
        ...providers,
        {
          ...custom,
          id: 'custom:inline',
          enabled: true,
          configured: true,
          builtin: false,
          logo_url: '',
          default_secret_name: '',
          default_allowed_hosts: [
            ...new Set([new URL(custom.authorize_endpoint).hostname, new URL(custom.token_endpoint).hostname]),
          ],
        },
      ]
    : providers;
  const selected = availableProviders.find((provider) => provider.id === values.envOAuthProviderId);
  const hostsMissing = credentialEnvHostsMissing(values);
  const options = availableProviders
    .filter((provider) => provider.enabled || (secretNameLocked && provider.id === values.envOAuthProviderId))
    .map((provider) => ({
      id: provider.id,
      label: provider.name,
      icon: <OAuthProviderAvatar logoUrl={provider.logo_url} />,
    }));
  const selectableOptions =
    secretNameLocked && selected ? options.filter((option) => option.id === selected.id) : options;
  const providerSaved = (provider: CustomOAuthProvider, editing: boolean) => {
    const allowedHosts = [
      ...new Set([new URL(provider.authorize_endpoint).hostname, new URL(provider.token_endpoint).hostname]),
    ].join('\n');
    onChange({
      envOAuthCustomProvider: provider,
      envOAuthProviderId: 'custom:inline',
      ...(!editing
        ? { allowedHostsText: allowedHosts, oauthClientId: '', oauthClientSecret: '', envOAuthClientSecretSaved: false }
        : {}),
    });
  };
  return (
    <div className="space-y-4">
      <CustomOAuthProviderPicker
        label={msg('managedAgents.credentialVaults.credentialDialog.envOAuthProvider', 'OAuth provider')}
        value={values.envOAuthProviderId}
        placeholder={
          providersLoading
            ? msg('managedAgents.credentialVaults.credentialDialog.envOAuthProvidersLoading', 'Loading providers...')
            : msg('managedAgents.credentialVaults.credentialDialog.envOAuthSelectProvider', 'Select a provider')
        }
        options={selectableOptions}
        canCreate={!secretNameLocked}
        customProvider={values.envOAuthProviderId === 'custom:inline' && custom ? custom : undefined}
        onChange={(providerId) => {
          if (secretNameLocked || providerId === values.envOAuthProviderId) {
            return;
          }
          const provider = availableProviders.find((item) => item.id === providerId);
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
      {selected && !selected.configured ? (
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
      {values.envOAuthProviderId === 'custom:inline' && custom ? (
        <EnvOAuthClientFields
          clientRef={clientFieldsRef}
          manualToken={Boolean(values.envOAuthManualToken.trim())}
          values={values}
          onChange={onChange}
          saved={secretNameLocked && values.envOAuthClientSecretSaved}
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
