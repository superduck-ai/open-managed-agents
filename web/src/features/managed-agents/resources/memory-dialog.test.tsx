import { afterEach, expect, mock, test } from 'bun:test';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { resetTestDom } from '../../../test/setup';
import { MemoryDialog } from './dialogs';

afterEach(cleanup);

test('keeps a pending memory submission open and preserves the draft after failure and through cancellation', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores/example');
  let failSave: (reason: Error) => void = () => {};
  const onSubmit = mock(
    () =>
      new Promise<void>((_resolve, reject) => {
        failSave = reject;
      }),
  );
  const onClose = mock(() => {});
  render(<MemoryDialog onSubmit={onSubmit} onClose={onClose} />);
  const dialog = screen.getByRole('dialog', { name: 'Add memory' });
  const path = within(dialog).getByLabelText('Path') as HTMLInputElement;
  const content = within(dialog).getByLabelText('Content') as HTMLTextAreaElement;
  fireEvent.change(path, { target: { value: '/notes/example.txt' } });
  fireEvent.change(content, { target: { value: 'Original memory content' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Add memory' }));
  await waitFor(() =>
    expect(within(dialog).getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true),
  );
  expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
  expect(path.closest('fieldset')?.disabled).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  fireEvent.submit(dialog.querySelector('form')!);
  expect(onSubmit).toHaveBeenCalledTimes(1);
  expect(onClose).toHaveBeenCalledTimes(0);
  await act(async () => failSave(new Error('Sample memory save failure')));
  expect((await within(dialog).findByRole('alert')).textContent).toBe('Sample memory save failure');
  expect(path.value).toBe('/notes/example.txt');
  expect(content.value).toBe('Original memory content');
  expect(path.closest('fieldset')?.disabled).toBe(false);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  expect(content.value).toBe('Original memory content');
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(onClose).toHaveBeenCalledTimes(1);
});
