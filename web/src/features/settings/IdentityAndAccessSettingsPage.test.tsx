import { afterEach, describe, expect, test } from 'bun:test';
import { I18nProvider } from '../../shared/i18n';
import { resetTestDom } from '../../test/setup';
import { IdentityAndAccessSettingsPage } from './IdentityAndAccessSettingsPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('Identity and access settings page', () => {
  test('opens configure dialogs and updates local preview defaults', async () => {
    resetTestDom('https://oma.duck.ai/settings/identity-and-access');

    const { container } = render(
      <I18nProvider initialLocale="en">
        <IdentityAndAccessSettingsPage />
      </I18nProvider>
    );

    expect(container.querySelectorAll('[data-slot="card"]').length).toBeGreaterThan(1);

    const configureButtons = () => screen.getAllByRole('button', { name: 'Configure' });

    press(configureButtons()[0]);
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Configure sign-in method' })).toBeTruthy());
    fireEvent.click(screen.getByRole('combobox', { name: 'Authentication mode' }));
    selectOption('SSO only');
    press(screen.getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: SSO and email verification')).toBeTruthy();

    press(configureButtons()[1]);
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Configure verified domain' })).toBeTruthy());
    fireEvent.change(screen.getByRole('textbox', { name: 'Verified domain' }), {
      target: { value: 'corp.example' }
    });
    press(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: corp.example')).toBeTruthy();

    press(screen.getByRole('switch', { name: 'Restrict invites to verified domains' }));
    expect(screen.getByText('Current default: Any domain can be invited')).toBeTruthy();

    press(screen.getByRole('switch', { name: 'Just-in-time provisioning' }));
    expect(screen.getByText('Current default: Manual invitations only')).toBeTruthy();

    press(configureButtons()[2]);
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Configure default invite role' })).toBeTruthy());
    press(screen.getByRole('button', { name: 'Close' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

    press(configureButtons()[2]);
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Configure default invite role' })).toBeTruthy());
    fireEvent.click(screen.getByRole('combobox', { name: 'Default role' }));
    selectOption('Admin');
    press(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: Admin')).toBeTruthy();
  });
});

function selectOption(name: string) {
  const option = screen.getByRole('option', { name });
  press(option);
}

function press(target: Element) {
  fireEvent.pointerDown(target);
  fireEvent.mouseDown(target);
  fireEvent.pointerUp(target);
  fireEvent.mouseUp(target);
  fireEvent.click(target);
}
