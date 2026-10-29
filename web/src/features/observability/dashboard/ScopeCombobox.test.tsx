import '../../../test/setup';
import { afterEach, expect, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { ScopeCombobox } from './ScopeCombobox';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

function renderCombobox(onChange: (value: string) => void = () => {}) {
  render(
    <I18nProvider initialLocale="en">
      <ScopeCombobox
        label="Agent"
        value=""
        allLabel="All agents"
        searchPlaceholder="Search agents"
        emptyLabel="No matches"
        options={[{ value: 'agent_1', label: 'Alpha' }]}
        onChange={onChange}
      />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'Agent' }));
}

test('names the search input with its placeholder', () => {
  renderCombobox();
  expect(screen.getByRole('combobox', { name: 'Search agents' })).toBeTruthy();
});

test('clears the search after selecting an option', () => {
  const selected: string[] = [];
  renderCombobox((value) => selected.push(value));
  fireEvent.change(screen.getByRole('combobox', { name: 'Search agents' }), { target: { value: 'Alp' } });
  fireEvent.click(screen.getByRole('option', { name: /Alpha/ }));
  expect(selected).toEqual(['agent_1']);
  fireEvent.click(screen.getByRole('combobox', { name: 'Agent' }));
  expect((screen.getByRole('combobox', { name: 'Search agents' }) as HTMLInputElement).value).toBe('');
});
