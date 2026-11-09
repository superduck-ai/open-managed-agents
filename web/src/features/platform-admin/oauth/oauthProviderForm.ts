import type { OAuthProviderRecord } from '../../../shared/api/platform-admin';

export type OAuthProviderFormValues = {
  name: string;
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
    name: provider?.builtin ? 'Feishu' : (provider?.name ?? ''),
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
  if ([values.name, values.authorizeEndpoint, values.tokenEndpoint, values.clientId].some((value) => !value.trim()))
    return false;
  const sameClient = provider?.configured && values.clientId.trim() === provider.client_id;
  if (!sameClient && !values.clientSecret.trim()) return false;
  return !provider?.builtin || values.scope.split(/\s+/).includes('offline_access');
}
