import { afterEach, expect, mock, test } from 'bun:test';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { EnvOAuthCredentialFields } from './credential-env-oauth-fields';
import type { CredentialFormValues, EnvOAuthProviderApiResponse } from '../types';

const providers: EnvOAuthProviderApiResponse[] = [
  {
    id: 'builtin:feishu',
    name: 'Feishu',
    builtin: true,
    configured: true,
    authorize_endpoint: 'https://accounts.feishu.cn/open-apis/authen/v1/authorize',
    token_endpoint: 'https://open.feishu.cn/open-apis/authen/v2/oauth/token',
    scope: 'offline_access',
    default_secret_name: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
    default_allowed_hosts: ['open.feishu.cn'],
  },
  {
    id: 'custom:example',
    name: 'Example',
    builtin: false,
    configured: true,
    authorize_endpoint: 'https://example.test/authorize',
    token_endpoint: 'https://example.test/token',
    scope: '',
    default_secret_name: '',
    default_allowed_hosts: ['example.test'],
  },
];

const values = {
  authType: 'env_oauth',
  envOAuthProviderId: 'custom:example',
  secretName: 'EXAMPLE_TOKEN',
  allowedHostsText: 'example.test',
  networkType: 'limited',
} as CredentialFormValues;

afterEach(cleanup);

test('keeps the provider fixed when editing an existing env_oauth credential', () => {
  const onChange = mock(() => {});
  render(
    <EnvOAuthCredentialFields
      values={values}
      providers={providers}
      providersLoading={false}
      providersError={null}
      onRetryProviders={() => {}}
      secretNameLocked
      onChange={onChange}
    />,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'OAuth provider' }));
  const options = screen.getAllByRole('option');
  expect(options.some((option) => option.textContent?.includes('Feishu'))).toBe(false);
  fireEvent.click(options[options.length - 1]);
  expect(onChange).not.toHaveBeenCalled();
  expect((screen.getByLabelText('Variable name') as HTMLInputElement).value).toBe('EXAMPLE_TOKEN');
});

test('shows the provider loading error and retries', () => {
  const onRetryProviders = mock(() => {});
  render(
    <EnvOAuthCredentialFields
      values={{ ...values, envOAuthProviderId: '' }}
      providers={[]}
      providersLoading={false}
      providersError="Could not load OAuth providers."
      onRetryProviders={onRetryProviders}
      secretNameLocked={false}
      onChange={() => {}}
    />,
  );
  expect(screen.getByText('Could not load OAuth providers.')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  expect(onRetryProviders).toHaveBeenCalledTimes(1);
});
