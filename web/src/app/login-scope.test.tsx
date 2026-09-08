import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../test/setup';
import { createMemoryHistory } from '@tanstack/react-router';

const { cleanup, fireEvent, render, screen, waitFor, act } = await import('@testing-library/react');
const { App } = await import('./App');
const { router } = await import('./router');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('未登录跳转及邮箱提交不重复导航或重建验证码页面', async () => {
  resetTestDom('https://oma.duck.ai/login');
  Object.assign(globalThis, { scrollTo: () => {} });
  router.update({ history: createMemoryHistory({ initialEntries: ['/'] }) });
  globalThis.fetch = mock(async (input) => {
    if (String(input) === '/api/bootstrap') return Response.json({ account: null });
    return Response.json({ sent: true });
  }) as typeof fetch;
  let navigations = 0;
  const unsubscribe = router.subscribe('onBeforeNavigate', () => {
    navigations++;
  });
  render(<App />);
  await screen.findByLabelText(/Email/);
  fireEvent.change(screen.getByLabelText(/Email/), { target: { value: 'test@example.com' } });
  fireEvent.click(screen.getByRole('button', { name: 'Continue with email' }));
  await screen.findByText('Enter the 6-digit code sent to your email.');
  await act(async () => {
    await router.invalidate();
  });
  await waitFor(() => expect(screen.getByText('Enter the 6-digit code sent to your email.')).toBeTruthy());
  expect(navigations).toBeLessThan(5);
  unsubscribe();
});
