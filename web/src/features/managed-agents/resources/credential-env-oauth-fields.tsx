import { useContext, useState } from 'react';
import { Plus } from 'lucide-react';
import { WorkspaceOAuthProviderDialog } from './workspace-oauth-provider-dialog';
import { Link } from '@tanstack/react-router';
import { AuthContext } from '../../../shared/auth/context';
import { useI18n } from '../../../shared/i18n';
import { OAuthProviderAvatar } from '../../../shared/icons/oauth-provider-avatar';
import { Button } from '../../../shared/ui/button';
import { FieldDescription } from '../../../shared/ui/field';
import { ManagedSelectField, ManagedTextField } from '../components/common';
import { type CredentialFormValues, type EnvOAuthProviderApiResponse } from '../types';
import { CredentialNetworkingFields } from './credential-networking-fields';
import { OptionalCredentialFields } from './optional-credential-fields';
import { credentialEnvHostsMissing, envOAuthProviderDefaults } from './model';

type EnvOAuthCredentialFieldsProps = {
  orgUuid?: string;
  workspaceId?: string;
  onProviderCreated?: (provider: EnvOAuthProviderApiResponse) => void;
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
  onProviderCreated,
  values,
  providers,
  providersLoading,
  providersError,
  onRetryProviders,
  secretNameLocked,
  onChange,
}: EnvOAuthCredentialFieldsProps) {
  const { msg } = useI18n();
  const auth = useContext(AuthContext);
  const platformAccess = auth?.platformAccess;
  const [creating, setCreating] = useState(false);
  const selected = providers.find((provider) => provider.id === values.envOAuthProviderId);
  const hostsMissing = credentialEnvHostsMissing(values);
  const options = providers
    .filter((provider) => provider.enabled || (secretNameLocked && provider.id === values.envOAuthProviderId))
    .map((provider) => ({
      id: provider.id,
      label: provider.name,
      icon: <OAuthProviderAvatar logoUrl={provider.logo_url} />,
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
        options={
          orgUuid && workspaceId && !secretNameLocked
            ? [
                ...selectableOptions,
                {
                  id: '__create_provider__',
                  label: msg('managedAgents.credentialVaults.credentialDialog.customProvider', 'Custom OAuth provider'),
                  icon: <Plus className="size-4" aria-hidden />,
                },
              ]
            : selectableOptions
        }
        onChange={(providerId) => {
          if (providerId === '__create_provider__') {
            setCreating(true);
            return;
          }
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
      {creating && orgUuid && workspaceId ? (
        <WorkspaceOAuthProviderDialog
          orgUuid={orgUuid}
          workspaceId={workspaceId}
          onClose={() => setCreating(false)}
          onCreated={(record) => {
            const provider = { ...record, id: record.provider_key };
            onProviderCreated?.(provider);
            const defaults = envOAuthProviderDefaults(provider);
            const previousDefault = selected ? envOAuthProviderDefaults(selected).secretName : '';
            onChange({
              envOAuthProviderId: provider.id,
              allowedHostsText: defaults.allowedHostsText,
              ...(values.secretName.trim() && values.secretName !== previousDefault
                ? {}
                : { secretName: defaults.secretName }),
            });
            setCreating(false);
          }}
        />
      ) : null}
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
      <FieldDescription>
        {msg(
          'managedAgents.credentialVaults.credentialDialog.envOAuthHint',
          'The access token is injected as an Authorization header for the allowed hosts and refreshed automatically when it expires.',
        )}
      </FieldDescription>
    </div>
  );
}
