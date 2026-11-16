import { afterEach, expect, mock, test } from 'bun:test';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { EnvOAuthCredentialFields } from './credential-env-oauth-fields';
import type { CredentialFormValues, EnvOAuthProviderApiResponse } from '../types';

const providers: EnvOAuthProviderApiResponse[] = [
  {
    id: 'builtin:feishu',
    name: 'Feishu',
    builtin: true,
    enabled: true,
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
    enabled: true,
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

test('replaces an auto-filled variable name and keeps a typed one when switching providers', () => {
  const onChange = mock(() => {});
  const { unmount } = render(
    <EnvOAuthCredentialFields
      values={{
        ...values,
        envOAuthProviderId: 'builtin:feishu',
        secretName: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
        allowedHostsText: 'open.feishu.cn',
      }}
      providers={providers}
      providersLoading={false}
      providersError={null}
      onRetryProviders={() => {}}
      secretNameLocked={false}
      onChange={onChange}
    />,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(screen.getAllByRole('option').find((option) => option.textContent?.includes('Example'))!);
  expect(onChange).toHaveBeenCalledWith({
    envOAuthProviderId: 'custom:example',
    allowedHostsText: 'example.test',
    secretName: '',
  });
  unmount();

  const typed = mock(() => {});
  render(
    <EnvOAuthCredentialFields
      values={{
        ...values,
        envOAuthProviderId: 'builtin:feishu',
        secretName: 'TEAM_FEISHU_TOKEN',
        allowedHostsText: 'open.feishu.cn',
      }}
      providers={providers}
      providersLoading={false}
      providersError={null}
      onRetryProviders={() => {}}
      secretNameLocked={false}
      onChange={typed}
    />,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(screen.getAllByRole('option').find((option) => option.textContent?.includes('Example'))!);
  expect(typed).toHaveBeenCalledWith({ envOAuthProviderId: 'custom:example', allowedHostsText: 'example.test' });
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

test.each([
  ['builtin:feishu', 'Feishu', 'LARKSUITE_CLI_USER_ACCESS_TOKEN'],
  ['custom:poap_example', 'Custom', 'CUSTOM_ACCESS_TOKEN'],
  ['custom:example', 'Example', ''],
])('未选服务方时提示通用变量名，选择 %s 后使用对应默认值', (id, name, secretName) => {
  const onChange = mock(() => {});
  const { rerender } = render(
    <EnvOAuthCredentialFields
      values={{ ...values, envOAuthProviderId: '', secretName: '' }}
      providers={[
        ...providers,
        {
          ...providers[0],
          id: 'custom:poap_example',
          builtin: false,
          name: 'Custom',
          default_secret_name: 'CUSTOM_ACCESS_TOKEN',
        },
      ]}
      providersLoading={false}
      providersError={null}
      onRetryProviders={() => {}}
      secretNameLocked={false}
      onChange={onChange}
    />,
  );
  const input = screen.getByLabelText('Variable name') as HTMLInputElement;
  expect(input.value).toBe('');
  expect(input.placeholder).toBe('ACCESS_TOKEN');
  fireEvent.click(screen.getByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(screen.getAllByRole('option').find((option) => option.textContent?.includes(name))!);
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ envOAuthProviderId: id, secretName }));
  rerender(
    <EnvOAuthCredentialFields
      values={{ ...values, envOAuthProviderId: id, secretName }}
      providers={providers}
      providersLoading={false}
      providersError={null}
      onRetryProviders={() => {}}
      secretNameLocked={false}
      onChange={onChange}
    />,
  );
  expect(input.value).toBe(secretName);
});

test('手填区只提供 Access token', () => {
  const onChange = mock(() => {});
  const { container } = render(
    <EnvOAuthCredentialFields
      values={{ ...values, envOAuthManualToken: 'existing-token' }}
      providers={providers}
      providersLoading={false}
      providersError={null}
      onRetryProviders={() => {}}
      secretNameLocked
      onChange={onChange}
    />,
  );
  const tokenInput = screen.getByLabelText('Access token');
  expect(container.querySelectorAll('input[type="password"]')).toHaveLength(1);
  expect(screen.queryByLabelText('Refresh token (optional)')).toBeNull();
  expect(screen.queryByLabelText('Expires at (optional)')).toBeNull();
  fireEvent.change(tokenInput, { target: { value: 'new-token' } });
  expect(onChange).toHaveBeenCalledWith({ envOAuthManualToken: 'new-token' });
});

test.each([false, true])('停用服务方仅对编辑已有凭证可见：%s', (editing) => {
  render(
    <EnvOAuthCredentialFields
      values={values}
      providers={providers.map((p) => ({ ...p, enabled: false }))}
      providersLoading={false}
      providersError={null}
      onRetryProviders={() => {}}
      secretNameLocked={editing}
      onChange={() => {}}
    />,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'OAuth provider' }));
  expect(screen.queryAllByRole('option').some((option) => option.textContent?.includes('Example'))).toBe(editing);
});
