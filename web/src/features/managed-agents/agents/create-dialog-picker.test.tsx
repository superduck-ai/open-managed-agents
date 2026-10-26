import '../../../test/setup';
import { afterEach, expect, mock, test } from 'bun:test';
import { useState } from 'react';
import { CreateDialogPicker, CreateDialogPickerList } from './create-dialog-picker';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(cleanup);

const options = [{ id: 'agent-research', label: 'Research assistant' }];

function ControlledPicker({ closeOnSelect }: { closeOnSelect: boolean }) {
  const [search, setSearch] = useState('');
  return (
    <CreateDialogPicker
      label="Add agent"
      placeholder="Add agent"
      searchPlaceholder="Search agents..."
      emptyLabel="No agents found."
      options={options}
      selectedIds={[]}
      onToggle={() => {}}
      searchValue={search}
      onSearchChange={setSearch}
      closeOnSelect={closeOnSelect}
    />
  );
}

for (const closeOnSelect of [false, true]) {
  test(`clears a controlled query after ${closeOnSelect ? 'selection' : 'dismissal'}`, async () => {
    render(<ControlledPicker closeOnSelect={closeOnSelect} />);
    const trigger = screen.getByRole('combobox', { name: 'Add agent' });
    fireEvent.click(trigger);
    fireEvent.change(screen.getByPlaceholderText('Search agents...'), { target: { value: 'Research' } });
    if (closeOnSelect) fireEvent.click(await screen.findByRole('option', { name: 'Research assistant' }));
    else fireEvent.click(trigger);
    await waitFor(() => expect(screen.queryByPlaceholderText('Search agents...')).toBeNull());
    fireEvent.click(trigger);
    expect(screen.getByPlaceholderText('Search agents...')).toHaveProperty('value', '');
  });
}

test('names the picker search from its supplied search placeholder', () => {
  render(
    <CreateDialogPickerList
      searchPlaceholder="Search agents..."
      emptyLabel="No agents found."
      options={options}
      selectedIds={[]}
      onToggle={() => {}}
    />,
  );

  expect(screen.getByRole('combobox', { name: 'Search agents...' })).toBeTruthy();
});

test('shows the supplied no-results state when filtering existing options', async () => {
  render(
    <CreateDialogPickerList
      searchPlaceholder="Search skills..."
      emptyLabel="No skills found."
      options={options}
      selectedIds={[]}
      onToggle={() => {}}
    />,
  );

  fireEvent.change(screen.getByPlaceholderText('Search skills...'), { target: { value: 'no-match' } });
  expect(await screen.findByText('No skills found.')).toBeTruthy();
  expect(screen.queryByRole('option', { name: 'Research assistant' })).toBeNull();
});

test('keeps a multi-select picker open after choosing an option', async () => {
  const toggle = mock();
  render(
    <CreateDialogPicker
      label="Add subagent"
      placeholder="Add subagent"
      searchPlaceholder="Search agents..."
      emptyLabel="No agents found."
      options={options}
      selectedIds={[]}
      onToggle={toggle}
    />,
  );

  fireEvent.click(screen.getByRole('combobox', { name: 'Add subagent' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Research assistant' }));
  expect(toggle).toHaveBeenCalledWith('agent-research');
  expect(screen.getByPlaceholderText('Search agents...')).toBeTruthy();
});
