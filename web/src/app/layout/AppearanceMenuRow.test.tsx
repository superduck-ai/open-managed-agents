import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { ThemeProvider } from '../../shared/theme/ThemeProvider';
import { Button } from '../../shared/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '../../shared/ui/dropdown-menu';
import { AppearanceMenuRow } from './AppearanceMenuRow';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(() => {
  cleanup();
  window.localStorage.removeItem('oma.theme.mode');
});

function mount(locale: Locale) {
  return render(
    <I18nProvider initialLocale={locale}>
      <ThemeProvider>
        <DropdownMenu>
          <DropdownMenuTrigger render={<Button />}>Account</DropdownMenuTrigger>
          <DropdownMenuContent>
            <AppearanceMenuRow />
          </DropdownMenuContent>
        </DropdownMenu>
      </ThemeProvider>
    </I18nProvider>,
  );
}

test.each([
  ['en', 'System', 'Light', 'Dark'],
  ['zh-CN', '跟随系统', '浅色', '深色'],
] as const)(
  'changes theme directly without navigating or closing the menu in %s',
  async (locale, system, light, dark) => {
    resetTestDom('https://oma.duck.ai/dashboard');
    window.localStorage.removeItem('oma.theme.mode');
    mount(locale);
    fireEvent.click(screen.getByRole('button', { name: 'Account' }));
    expect(screen.getByRole('menuitemradio', { name: system }).getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByRole('menuitemradio', { name: dark }));
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('dark'));
    expect(screen.getByRole('menuitemradio', { name: dark }).getAttribute('aria-checked')).toBe('true');
    expect(window.localStorage.getItem('oma.theme.mode')).toBe('dark');
    fireEvent.click(screen.getByRole('menuitemradio', { name: light }));
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('light'));
    expect(screen.getByRole('menuitemradio', { name: light }).getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByRole('menuitemradio', { name: system }));
    await waitFor(() => expect(document.documentElement.dataset.themeMode).toBe('system'));
    expect(screen.getByRole('menu')).toBeTruthy();
    expect(window.location.pathname).toBe('/dashboard');
  },
);

test('restores the chosen theme when the menu is mounted again', async () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  window.localStorage.setItem('oma.theme.mode', 'light');
  const first = mount('en');
  fireEvent.click(screen.getByRole('button', { name: 'Account' }));
  fireEvent.click(screen.getByRole('menuitemradio', { name: 'Dark' }));
  await waitFor(() => expect(window.localStorage.getItem('oma.theme.mode')).toBe('dark'));
  first.unmount();
  mount('en');
  fireEvent.click(screen.getByRole('button', { name: 'Account' }));
  expect(screen.getByRole('menuitemradio', { name: 'Dark' }).getAttribute('aria-checked')).toBe('true');
  expect(document.documentElement.dataset.theme).toBe('dark');
});
