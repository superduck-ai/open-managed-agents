import { useContext } from 'react';
import { Link } from '@tanstack/react-router';
import { AuthContext } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { FieldDescription } from '../../../shared/ui/field';
import { ManagedSelectField, ManagedTextField } from '../components/common';
import { type CredentialFormValues, type EnvOAuthProviderApiResponse } from '../types';
import { CredentialNetworkingFields } from './credential-networking-fields';
import { credentialEnvHostsMissing, envOAuthProviderDefaults } from './model';

type EnvOAuthCredentialFieldsProps = {
  values: CredentialFormValues;
  providers: EnvOAuthProviderApiResponse[];
  providersLoading: boolean;
  providersError: string | null;
  onRetryProviders: () => void;
  secretNameLocked: boolean;
  onChange: (patch: Partial<CredentialFormValues>) => void;
};

export function EnvOAuthCredentialFields({
  values,
  providers,
  providersLoading,
  providersError,
  onRetryProviders,
  secretNameLocked,
  onChange,
}: EnvOAuthCredentialFieldsProps) {
  const { msg } = useI18n();
  const platformAccess = useContext(AuthContext)?.platformAccess;
  const selected = providers.find((provider) => provider.id === values.envOAuthProviderId);
  const hostsMissing = credentialEnvHostsMissing(values);
  const options = providers.map((provider) => ({
    id: provider.id,
    label: provider.builtin
      ? msg('managedAgents.credentialVaults.credentialDialog.envOAuthBuiltinProvider', 'Built-in: {name}', {
          name: msg(`platformAdmin.oauthManagement.${provider.id.slice(8)}`, provider.name),
        })
      : provider.name,
    secondary: provider.scope || undefined,
  }));
  const selectableOptions =
    secretNameLocked && selected ? options.filter((option) => option.id === selected.id) : options;
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
        options={selectableOptions}
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
            'Not configured. Contact a platform administrator. You can still save a manually supplied token.',
          )}
          {platformAccess?.permissions.includes('platform:access') ? (
            <Link className="ml-2 underline" to="/platform-admin/oauth">
              {msg('platformAdmin.oauthManagement.configure', 'Configure provider')}
            </Link>
          ) : null}
        </FieldDescription>
      ) : null}
      <ManagedTextField
        label={msg('managedAgents.credentialVaults.credentialDialog.variableName', 'Variable name')}
        value={values.secretName}
        placeholder={
          selected?.id === 'builtin:friday'
            ? 'ACCESS_TOKEN'
            : msg(
                'managedAgents.credentialVaults.credentialDialog.envOAuthSecretNamePlaceholder',
                'LARKSUITE_CLI_USER_ACCESS_TOKEN',
              )
        }
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
