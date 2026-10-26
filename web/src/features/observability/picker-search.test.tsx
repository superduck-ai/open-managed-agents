import '../../test/setup';
import { afterEach, expect, test } from 'bun:test';
import { I18nProvider } from '../../shared/i18n';
import { ScopeCombobox } from './ScopeCombobox';
import { VersionCombobox } from './VersionCombobox';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
afterEach(cleanup);

const shared = {
  allLabel: 'All',
  emptyLabel: 'No matches',
  clearLabel: 'Clear selection',
  options: [],
};

for (const kind of ['scope', 'version']) {
  test(`names the ${kind} search using its supplied search label`, () => {
    render(
      <I18nProvider initialLocale="en">
        {kind === 'scope' ? (
          <ScopeCombobox {...shared} label="Agent" searchPlaceholder="Search agents" value="" onChange={() => {}} />
        ) : (
          <VersionCombobox
            {...shared}
            label="Version"
            searchPlaceholder="Search versions"
            values={[]}
            onChange={() => {}}
          />
        )}
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole('combobox', { name: kind === 'scope' ? 'Agent' : 'Version' }));
    expect(screen.getByRole('combobox', { name: kind === 'scope' ? 'Search agents' : 'Search versions' })).toBeTruthy();
  });
}

for (const action of ['selectScope', 'clearScope', 'clearVersion', 'allVersion']) {
  test(`clears the search after ${action}`, async () => {
    const options = [{ id: '1', label: 'Alpha' }];
    render(
      <I18nProvider initialLocale="en">
        {action.endsWith('Version') ? (
          <VersionCombobox
            {...shared}
            options={options}
            label="Version"
            searchPlaceholder="Search"
            values={[1]}
            onChange={() => {}}
          />
        ) : (
          <ScopeCombobox
            {...shared}
            options={options}
            label="Agent"
            searchPlaceholder="Search"
            value="1"
            onChange={() => {}}
          />
        )}
      </I18nProvider>,
    );
    const trigger = screen.getByRole('combobox', { name: action.endsWith('Version') ? 'Version' : 'Agent' });
    fireEvent.click(trigger);
    fireEvent.change(screen.getByPlaceholderText('Search'), {
      target: { value: action === 'allVersion' ? 'All' : 'Alpha' },
    });
    if (action === 'allVersion') fireEvent.click(screen.getByRole('option', { name: 'All' }));
    else if (action === 'selectScope') fireEvent.click(screen.getByRole('option', { name: /Alpha/ }));
    else fireEvent.pointerDown(screen.getByLabelText('Clear selection'));
    await waitFor(() => expect(screen.queryByPlaceholderText('Search')).toBeNull());
    fireEvent.click(trigger);
    await waitFor(() => expect((screen.getByPlaceholderText('Search') as HTMLInputElement).value).toBe(''));
  });
}

for (const kind of ['scope', 'version']) {
  test(`starts keyboard selection from the current ${kind}`, async () => {
    const options = [
      { id: '1', label: 'Alpha' },
      { id: '2', label: 'Beta' },
    ];
    const changes: unknown[] = [];
    render(
      <I18nProvider initialLocale="en">
        {kind === 'scope' ? (
          <ScopeCombobox
            {...shared}
            options={options}
            label="Agent"
            searchPlaceholder="Search"
            value="2"
            onChange={(value) => changes.push(value)}
          />
        ) : (
          <VersionCombobox
            {...shared}
            options={options}
            label="Version"
            searchPlaceholder="Search"
            values={[2, 1]}
            onChange={(value) => changes.push(value)}
          />
        )}
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole('combobox', { name: kind === 'scope' ? 'Agent' : 'Version' }));
    await waitFor(() =>
      expect(screen.getByRole('option', { name: 'Beta' }).getAttribute('data-selected')).toBe('true'),
    );
    const originalEvent = globalThis.Event;
    try {
      globalThis.Event = window.Event;
      fireEvent.keyDown(screen.getByPlaceholderText('Search'), { key: 'Enter' });
    } finally {
      globalThis.Event = originalEvent;
    }
    expect(changes).toEqual(kind === 'scope' ? ['2'] : [[1]]);
  });
}
