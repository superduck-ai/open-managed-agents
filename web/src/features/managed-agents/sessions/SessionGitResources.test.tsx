import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { setAnthropicClientForTest } from '@/shared/api/anthropic';
import { SessionGitResources } from './SessionGitResources';

const { act, cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setAnthropicClientForTest(null);
});

function renderRepository(archived = false) {
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
      archived={archived}
    />,
  );
}

test('blocks repeated submissions and closing while saving, then keeps the failed draft through exit', async () => {
  resetTestDom('https://oma.duck.ai/');
  let finishSave: (response: Response) => void = () => {};
  let calls = 0;
  globalThis.fetch = (async () => {
    calls++;
    return new Promise<Response>((resolve) => {
      finishSave = resolve;
    });
  }) as typeof fetch;
  setAnthropicClientForTest(null);
  renderRepository();
  const trigger = screen.getByRole('button', { name: 'Update authorization token' });
  fireEvent.click(trigger);
  const dialog = await screen.findByRole('dialog');
  const password = screen.getByLabelText('Authorization token');
  fireEvent.change(password, { target: { value: 'fixture-only' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  fireEvent.submit(dialog.querySelector('form')!);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(screen.getByRole('dialog')).toBe(dialog);
  expect(screen.getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
  await waitFor(() => expect(calls).toBe(1));
  await act(async () => finishSave(Response.json({ error: { message: 'Fixture save failed' } }, { status: 503 })));
  const error = await screen.findByRole('alert');
  expect(error.textContent).toBe('Fixture save failed');
  expect(document.activeElement).toBe(error);
  expect(password).toHaveProperty('value', 'fixture-only');
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  expect(dialog.hasAttribute('data-closed')).toBe(true);
  expect(dialog.textContent).toContain('https://github.com/owner/repo');
  expect(password).toHaveProperty('value', 'fixture-only');
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  await waitFor(() => expect(document.activeElement).toBe(trigger));
  fireEvent.click(trigger);
  expect(await screen.findByLabelText('Authorization token')).toHaveProperty('value', '');
});

test('does not open token editing for an archived session', () => {
  resetTestDom('https://oma.duck.ai/');
  renderRepository(true);
  const trigger = screen.getByRole('button', { name: 'Update authorization token' });
  expect(trigger.hasAttribute('disabled')).toBe(true);
  fireEvent.click(trigger);
  expect(screen.queryByRole('dialog')).toBeNull();
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
  renderRepository();
  expect(screen.getByText('/workspace/repo')).toBeTruthy();
  expect(screen.getByText('branch: main')).toBeTruthy();
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
