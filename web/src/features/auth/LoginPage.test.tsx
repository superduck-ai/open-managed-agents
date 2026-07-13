import { afterEach, describe, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { I18nProvider } from '../../shared/i18n';

const testingLibrary = await import('@testing-library/react');
const { LoginFlow } = await import('./LoginPage');

const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('LoginFlow', () => {
  test('renders email-only Open Managed Agents login without Google, SSO, or Contact sales', () => {
    resetTestDom('https://oma.duck.ai/login');
    render(<LoginFlow onAuthenticated={() => undefined} />);

    expect(screen.getAllByText('Open Managed Agents').length).toBeGreaterThan(0);
    expect(screen.getByText('Build with Open Managed Agents')).toBeTruthy();
    expect(screen.getByRole('link', { name: /Developer Docs/i }).getAttribute('data-slot')).toBe('button');
    expect(document.querySelectorAll('[data-slot="card"]').length).toBe(1);
    expect(document.querySelector('.surface-card')).toBeNull();
    expect(screen.queryByText('Passwordless sign-in')).toBeNull();
    expect(screen.queryByText('Built for the managed agent console')).toBeNull();
    expect(
      screen.queryByText(
        'Use a one-time email code to access workspaces, agents, sessions, and analytics in the same console flow.',
      ),
    ).toBeNull();
    expect(screen.queryByText('Email-first access')).toBeNull();
    expect(screen.queryByText('Return where you left off')).toBeNull();
    expect(screen.queryByText('Server-backed sessions')).toBeNull();
    expect(screen.queryByText(/Google/i)).toBeNull();
    expect(screen.queryByText(/\bSSO\b/i)).toBeNull();
    expect(screen.queryByText(/Contact sales/i)).toBeNull();
  });

  test('submits email and advances to verification code step', async () => {
    resetTestDom('https://oma.duck.ai/login');
    const send = mock(async () => ({ sent: true }));
    render(<LoginFlow onAuthenticated={() => undefined} onSendMagicLink={send} />);

    fireEvent.input(screen.getByLabelText(/Email/i), { target: { value: 'ada@example.com' } });
    fireEvent.click(screen.getByRole('button', { name: /Continue with email/i }));

    await waitFor(() => expect(send).toHaveBeenCalledWith('ada@example.com'));
    expect(screen.getByText(/We sent a 6-digit code to/i)).toBeTruthy();
  });

  test('verifies code and completes authentication', async () => {
    resetTestDom('https://oma.duck.ai/login');
    const send = mock(async () => ({ sent: true }));
    const verify = mock(async () => ({ success: true }));
    const authenticated = mock(async () => undefined);

    render(<LoginFlow onAuthenticated={authenticated} onSendMagicLink={send} onVerifyMagicLink={verify} />);

    fireEvent.input(screen.getByLabelText(/Email/i), { target: { value: 'grace@example.com' } });
    fireEvent.click(screen.getByRole('button', { name: /Continue with email/i }));

    await screen.findByLabelText(/Verification code/i);
    fireEvent.input(screen.getByLabelText(/Verification code/i), { target: { value: '123456' } });
    fireEvent.click(screen.getByRole('button', { name: /Verify email/i }));

    await waitFor(() => expect(verify).toHaveBeenCalledWith('grace@example.com', '123456'));
    await waitFor(() => expect(authenticated).toHaveBeenCalled());
  });

  test('does not render the removed marketing footer', () => {
    resetTestDom('https://oma.duck.ai/login');
    render(<LoginFlow onAuthenticated={() => undefined} />);
    const page = within(document.body);

    expect(page.queryByText('Contact sales')).toBeNull();
    expect(page.queryByText('Products')).toBeNull();
    expect(page.queryByText('Company')).toBeNull();
    expect(document.querySelector('footer')).toBeNull();
  });

  test('shows validation errors with the shared alert surface', async () => {
    resetTestDom('https://oma.duck.ai/login');
    render(<LoginFlow onAuthenticated={() => undefined} />);

    fireEvent.input(screen.getByLabelText(/Email/i), { target: { value: 'bad-email' } });
    fireEvent.submit(screen.getByRole('button', { name: /Continue with email/i }).closest('form') as HTMLFormElement);

    const alert = await screen.findByRole('alert');
    expect(alert.getAttribute('data-slot')).toBe('alert');
    expect(alert.textContent).toContain('Enter a valid email address.');
  });

  test('renders the login flow in Chinese when zh-CN is selected', () => {
    resetTestDom('https://oma.duck.ai/login');
    render(
      <I18nProvider initialLocale="zh-CN">
        <LoginFlow onAuthenticated={() => undefined} />
      </I18nProvider>,
    );

    expect(document.documentElement.lang).toBe('zh-CN');
    expect(screen.getByText('使用 Open Managed Agents 构建')).toBeTruthy();
    expect(screen.getByLabelText(/邮箱/i)).toBeTruthy();
    expect(screen.getByRole('button', { name: '使用邮箱继续' })).toBeTruthy();
  });
});
