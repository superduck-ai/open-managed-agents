import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { setAnthropicClientForTest } from '@/shared/api/anthropic';
import { SessionGitResources } from './SessionGitResources';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setAnthropicClientForTest(null);
});

test('displays a repository and rotates only its token, clearing the password after success', async () => {
  resetTestDom('https://oma.duck.ai/');
  let requestURL = '';
  let body: unknown;
  globalThis.fetch = (async (input, init) => {
    requestURL = String(input);
    body = JSON.parse(String(init?.body));
    return Response.json({ id: 'sesrsc_git', type: 'github_repository', url: 'https://github.com/owner/repo' });
  }) as typeof fetch;
  setAnthropicClientForTest(null);
  render(
    <SessionGitResources
      resources={[
        {
          id: 'sesrsc_git',
          type: 'github_repository',
          url: 'https://github.com/owner/repo',
          mount_path: '/workspace/repo',
          checkout: { type: 'branch', name: 'main' },
        },
      ]}
      sessionId="sesn_test"
      workspaceId="default"
      archived={false}
    />,
  );
  expect(screen.getByText('/workspace/repo · branch: main')).toBeTruthy();
  expect(screen.queryByRole('button', { name: /Remove|Add/ })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Update authorization token' }));
  const password = await screen.findByLabelText('Authorization token');
  expect(password.getAttribute('type')).toBe('password');
  expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(false);
  fireEvent.change(password, { target: { value: 'replacement-token' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(screen.getByRole('status').textContent).toBe('Authorization token updated.'));
  expect(requestURL).toBe('/v1/sessions/sesn_test/resources/sesrsc_git?beta=true');
  expect(body).toEqual({ authorization_token: 'replacement-token' });
  fireEvent.click(screen.getByRole('button', { name: 'Update authorization token' }));
  expect(((await screen.findByLabelText('Authorization token')) as HTMLInputElement).value).toBe('');
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(body).toEqual({ authorization_token: '' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});
