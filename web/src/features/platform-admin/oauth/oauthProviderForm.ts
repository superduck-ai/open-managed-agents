import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';

export type OAuthProviderFormValues = {
  name: string;
  defaultSecretName: string;
  defaultAllowedHostsText: string;
  logoUrl: string;
  authorizeEndpoint: string;
  tokenEndpoint: string;
  scope: string;
  clientId: string;
  clientSecret: string;
  authMethod: string;
  encoding: string;
};

export function initialOAuthProviderFormValues(provider?: OAuthProviderRecord): OAuthProviderFormValues {
  const {
    name = '',
    default_secret_name: defaultSecretName = '',
    default_allowed_hosts: defaultAllowedHosts,
    logo_url: logoUrl = '',
    authorize_endpoint: authorizeEndpoint = '',
    token_endpoint: tokenEndpoint = '',
    scope = '',
    client_id: clientId = '',
    token_endpoint_auth_method: authMethod = 'client_secret_post',
    token_endpoint_encoding: encoding = 'form',
  } = provider ?? {};
  return {
    name,
    defaultSecretName,
    defaultAllowedHostsText: defaultAllowedHosts?.join('\n') ?? '',
    logoUrl,
    authorizeEndpoint,
    tokenEndpoint,
    scope,
    clientId,
    clientSecret: '',
    authMethod,
    encoding,
  };
}

export function oauthProviderFormReady(values: OAuthProviderFormValues, provider?: OAuthProviderRecord): boolean {
  if ([values.name, values.authorizeEndpoint, values.tokenEndpoint].some((value) => !value.trim())) return false;
  const clientId = values.clientId.trim();
  const clientSecret = values.clientSecret.trim();
  if (provider?.configured) {
    if (!clientId || (clientId !== provider.client_id && !clientSecret)) return false;
  } else if (Boolean(clientId) !== Boolean(clientSecret)) return false;
  return true;
}

export function oauthProviderOptionalFields(values: OAuthProviderFormValues, provider?: OAuthProviderRecord) {
  return {
    clientId: !provider?.configured && !values.clientSecret.trim(),
    clientSecret: provider?.configured ? values.clientId.trim() === provider.client_id : !values.clientId.trim(),
    scope: true,
  };
}
