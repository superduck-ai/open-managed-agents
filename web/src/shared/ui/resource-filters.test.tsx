import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { useState } from 'react';
import { I18nProvider } from '../i18n';
import { ResourceFilterDropdown } from './resource-filters';
const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
afterEach(cleanup);
function Sample() {
  const [value, setValue] = useState('active');
  const [menu, setMenu] = useState<string | null>(null);
  return (
    <I18nProvider initialLocale="en">
      <ResourceFilterDropdown
        label="Status"
        valueLabel={value === 'active' ? 'Active' : 'Archived'}
        value={value}
        options={[
          { value: 'active', label: 'Active' },
          { value: 'archived', label: 'Archived' },
        ]}
        menu="status"
        open={menu === 'status'}
        menuWidthClass="w-56"
        onOpenChange={setMenu}
        onSelect={setValue}
      />
    </I18nProvider>
  );
}
test('controlled filters preserve their value on cancellation and search before selection', async () => {
  render(<Sample />);
  fireEvent.click(screen.getByRole('combobox', { name: 'Status Active' }));
  fireEvent.change(screen.getByPlaceholderText('Search options...'), { target: { value: 'missing' } });
  expect(await screen.findByText('No matching options.')).toBeTruthy();
  fireEvent.keyDown(screen.getByPlaceholderText('Search options...'), { key: 'Escape', code: 'Escape' });
  await waitFor(() => expect(screen.queryByPlaceholderText('Search options...')).toBeNull());
  fireEvent.click(screen.getByRole('combobox', { name: 'Status Active' }));
  expect(screen.getByPlaceholderText('Search options...')).toHaveProperty('value', '');
  fireEvent.change(screen.getByPlaceholderText('Search options...'), { target: { value: 'Archived' } });
  fireEvent.click(await screen.findByRole('option', { name: 'Archived' }));
  await waitFor(() => expect(screen.queryByPlaceholderText('Search options...')).toBeNull());
  expect(screen.getByRole('combobox', { name: 'Status Archived' })).toBeTruthy();
});
