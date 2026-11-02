import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { useState } from 'react';
import { I18nProvider } from '../i18n';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from './select';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(cleanup);

function Sample() {
  const [value, setValue] = useState('one');
  return (
    <I18nProvider initialLocale="en">
      <Select value={value} onValueChange={(next) => next !== null && setValue(next)}>
        <SelectTrigger aria-label="Environment">
          <SelectValue>{value || 'None'}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="">None</SelectItem>
          <SelectItem value="one">Research environment</SelectItem>
          <SelectItem value="two">Production environment</SelectItem>
          <SelectItem value="disabled" disabled>
            Unavailable environment
          </SelectItem>
        </SelectContent>
      </Select>
    </I18nProvider>
  );
}

test('searches displayed labels and preserves the selected value when no options match', async () => {
  render(<Sample />);
  fireEvent.click(screen.getByRole('combobox', { name: 'Environment' }));
  const search = screen.getByPlaceholderText('Search options...');
  fireEvent.change(search, { target: { value: 'missing' } });
  expect(await screen.findByText('No matching options.')).toBeTruthy();
  expect(screen.queryByRole('option', { name: 'Research environment' })).toBeNull();
  expect(screen.getByRole('combobox', { name: 'Environment' }).textContent).toBe('one');
  fireEvent.change(search, { target: { value: 'Production' } });
  fireEvent.click(await screen.findByRole('option', { name: 'Production environment' }));
  await waitFor(() => expect(screen.queryByPlaceholderText('Search options...')).toBeNull());
  expect(screen.getByRole('combobox', { name: 'Environment' }).textContent).toBe('two');
});

test('supports empty values and starts each reopening with an unfiltered search', async () => {
  render(<Sample />);
  const trigger = screen.getByRole('combobox', { name: 'Environment' });
  fireEvent.click(trigger);
  fireEvent.change(screen.getByPlaceholderText('Search options...'), { target: { value: 'None' } });
  fireEvent.click(await screen.findByRole('option', { name: 'None', exact: true }));
  await waitFor(() => expect(screen.queryByPlaceholderText('Search options...')).toBeNull());
  expect(trigger.textContent).toBe('None');
  fireEvent.click(trigger);
  expect(screen.getByPlaceholderText('Search options...')).toHaveProperty('value', '');
  expect(await screen.findByRole('option', { name: 'Research environment' })).toBeTruthy();
  expect(screen.getByRole('option', { name: 'Unavailable environment' }).getAttribute('aria-disabled')).toBe('true');
});

test('renders an uncontrolled selected label from the provided items', () => {
  render(
    <I18nProvider initialLocale="en">
      <Select defaultValue="admin" items={[{ value: 'admin', label: 'Administrator' }]} disabled>
        <SelectTrigger aria-label="Role">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="admin">Administrator</SelectItem>
        </SelectContent>
      </Select>
    </I18nProvider>,
  );
  const trigger = screen.getByRole('combobox', { name: 'Role' });
  expect(trigger.querySelector('[data-slot="select-value"]')?.textContent).toBe('Administrator');
  fireEvent.click(trigger);
  expect(screen.queryByPlaceholderText('Search options...')).toBeNull();
});

test('keeps equal option names distinct and returns the original value', async () => {
  let selected: string | null = null;
  render(
    <I18nProvider initialLocale="en">
      <Select
        onValueChange={(value) => {
          selected = value;
        }}
      >
        <SelectTrigger aria-label="Duplicate names">
          <SelectValue placeholder="Choose" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="first">
            <span>Research</span>
          </SelectItem>
          <SelectItem value="second">
            <span>Research</span>
          </SelectItem>
        </SelectContent>
      </Select>
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'Duplicate names' }));
  fireEvent.change(screen.getByPlaceholderText('Search options...'), { target: { value: 'Research' } });
  const matches = await screen.findAllByRole('option', { name: 'Research', exact: true });
  expect(matches).toHaveLength(2);
  fireEvent.click(matches[1]!);
  expect(selected).toBe('second');
});
