import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';

export type OAuthProviderFormValues = {
  name: string;
  defaultSecretName: string;
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
  return {
    name: provider?.name ?? '',
    defaultSecretName: provider?.default_secret_name ?? '',
    logoUrl: provider?.logo_url ?? '',
    authorizeEndpoint: provider?.authorize_endpoint ?? '',
    tokenEndpoint: provider?.token_endpoint ?? '',
    scope: provider?.scope ?? '',
    clientId: provider?.client_id ?? '',
    clientSecret: '',
    authMethod: provider?.token_endpoint_auth_method ?? 'client_secret_post',
    encoding: provider?.token_endpoint_encoding ?? 'form',
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
