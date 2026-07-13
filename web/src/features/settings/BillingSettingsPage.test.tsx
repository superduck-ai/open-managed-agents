import { afterEach, describe, expect, test } from 'bun:test';
import { I18nProvider } from '../../shared/i18n';
import { resetTestDom } from '../../test/setup';
import { BillingSettingsPage } from './BillingSettingsPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('Billing settings page', () => {
  test('opens configure dialogs and handles cancel, save, and close actions', async () => {
    resetTestDom('https://oma.duck.ai/settings/billing');

    const { container } = render(
      <I18nProvider initialLocale="en">
        <BillingSettingsPage />
      </I18nProvider>,
    );

    expect(container.querySelectorAll('[data-slot="card"]').length).toBeGreaterThan(1);

    const configureButtons = () => screen.getAllByRole('button', { name: 'Configure' });

    press(configureButtons()[0]);
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Configure invoice delivery' })).toBeTruthy());
    fireEvent.click(screen.getByRole('combobox', { name: 'Recipients' }));
    selectOption('All members');
    press(screen.getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: Admins and billing members')).toBeTruthy();

    press(configureButtons()[1]);
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Configure cost visibility' })).toBeTruthy());
    fireEvent.click(screen.getByRole('combobox', { name: 'Visibility' }));
    selectOption('All members');
    press(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: All members')).toBeTruthy();

    press(configureButtons()[2]);
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Configure billing digest cadence' })).toBeTruthy());
    press(screen.getByRole('button', { name: 'Close' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
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
