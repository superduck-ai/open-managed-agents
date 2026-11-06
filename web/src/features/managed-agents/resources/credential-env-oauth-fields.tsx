import { useI18n } from '../../../shared/i18n';
import { FieldDescription } from '../../../shared/ui/field';
import { ManagedSelectField, ManagedTextField } from '../components/common';
import { type CredentialFormValues, type EnvOAuthProviderApiResponse } from '../types';
import { CredentialNetworkingFields } from './credential-networking-fields';
import { credentialEnvHostsMissing, envOAuthProviderDefaults } from './model';

type EnvOAuthCredentialFieldsProps = {
  values: CredentialFormValues;
  providers: EnvOAuthProviderApiResponse[];
  providersLoading: boolean;
  secretNameLocked: boolean;
  onChange: (patch: Partial<CredentialFormValues>) => void;
};

export function EnvOAuthCredentialFields({
  values,
  providers,
  providersLoading,
  secretNameLocked,
  onChange,
}: EnvOAuthCredentialFieldsProps) {
  const { msg } = useI18n();
  const hostsMissing = credentialEnvHostsMissing(values);
  const options = providers.map((provider) => ({
    id: provider.id,
    label: provider.builtin
      ? msg('managedAgents.credentialVaults.credentialDialog.envOAuthBuiltinProvider', 'Built-in: {name}', {
          name: provider.name,
        })
      : provider.name,
    secondary: provider.scope || undefined,
  }));
  return (
    <div className="space-y-4">
      <ManagedSelectField
        label={msg('managedAgents.credentialVaults.credentialDialog.envOAuthProvider', 'OAuth provider')}
        value={values.envOAuthProviderId}
        placeholder={
          providersLoading
            ? msg('managedAgents.credentialVaults.credentialDialog.envOAuthProvidersLoading', 'Loading providers...')
            : msg('managedAgents.credentialVaults.credentialDialog.envOAuthSelectProvider', 'Select a provider')
        }
        options={options}
        onChange={(providerId) => {
          const provider = providers.find((item) => item.id === providerId);
          const defaults = provider ? envOAuthProviderDefaults(provider) : null;
          onChange({
            envOAuthProviderId: providerId,
            ...(defaults
              ? {
                  allowedHostsText: defaults.allowedHostsText,
                  ...(secretNameLocked || values.secretName.trim() ? {} : { secretName: defaults.secretName }),
                }
              : {}),
          });
        }}
      />
      <ManagedTextField
        label={msg('managedAgents.credentialVaults.credentialDialog.variableName', 'Variable name')}
        value={values.secretName}
        placeholder={msg(
          'managedAgents.credentialVaults.credentialDialog.envOAuthSecretNamePlaceholder',
          'LARKSUITE_CLI_USER_ACCESS_TOKEN',
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
      <div className="grid gap-4 sm:grid-cols-2">
        <ManagedTextField
          label={msg('managedAgents.credentialVaults.credentialDialog.envOAuthManualToken', 'Access token (optional)')}
          type="password"
          value={values.envOAuthManualToken}
          placeholder={msg(
            'managedAgents.credentialVaults.credentialDialog.envOAuthManualTokenPlaceholder',
            'Paste a token to skip authorization',
          )}
          onChange={(envOAuthManualToken) => onChange({ envOAuthManualToken })}
        />
        <ManagedTextField
          label={msg(
            'managedAgents.credentialVaults.credentialDialog.envOAuthRefreshToken',
            'Refresh token (optional)',
          )}
          type="password"
          value={values.envOAuthRefreshToken}
          onChange={(envOAuthRefreshToken) => onChange({ envOAuthRefreshToken })}
        />
      </div>
      <ManagedTextField
        label={msg('managedAgents.credentialVaults.credentialDialog.envOAuthExpiresAt', 'Expires at (optional)')}
        value={values.envOAuthExpiresAt}
        placeholder="2026-11-06T10:00:00Z"
        onChange={(envOAuthExpiresAt) => onChange({ envOAuthExpiresAt })}
      />
      <FieldDescription>
        {msg(
          'managedAgents.credentialVaults.credentialDialog.envOAuthHint',
          'The access token is injected as an Authorization header for the allowed hosts and refreshed automatically when it expires.',
        )}
      </FieldDescription>
    </div>
  );
}
