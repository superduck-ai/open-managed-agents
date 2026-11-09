import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { useState } from 'react';
import { I18nProvider } from '../../shared/i18n';
import { MCPServerEditor, MCPServerPanel } from './MCPServerDialogs';

const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);

function Sample({ loading }: { loading: boolean }) {
  const [target, setTarget] = useState<{ serverId: string } | null>(null);
  const [closed, setClosed] = useState(0);
  return (
    <I18nProvider initialLocale="en">
      <button onClick={() => setTarget({ serverId: 'mcpsrv_sample' })}>Open server</button>
      <p role="status">Closed {closed}</p>
      <MCPServerPanel
        target={target}
        detailLoading={loading}
        detailError={loading ? null : new Error('Unavailable')}
        formatter={new Intl.DateTimeFormat('en')}
        onClose={() => {
          setTarget(null);
          setClosed((count) => count + 1);
        }}
        onRetry={() => {}}
        onEdit={() => {}}
        onDelete={() => {}}
      />
    </I18nProvider>
  );
}

for (const loading of [true, false]) {
  test(`closes ${loading ? 'loading' : 'failed'} server details once and reopens the same server`, async () => {
    render(<Sample loading={loading} />);
    fireEvent.click(screen.getByRole('button', { name: 'Open server' }));
    const panel = await screen.findByRole('dialog');
    fireEvent.keyDown(panel, { key: 'Escape', code: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Closed 1')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Open server' }));
    const reopened = await screen.findByRole('dialog');
    fireEvent.click(within(reopened).getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Closed 2')).toBeTruthy();
  });
}

function EditorSample({ onSubmit }: { onSubmit: () => Promise<void> }) {
  const [open, setOpen] = useState(false);
  const [closed, setClosed] = useState(0);
  return (
    <I18nProvider initialLocale="en">
      <button onClick={() => setOpen(true)}>Open editor</button>
      <p role="status">Closed {closed}</p>
      <MCPServerEditor
        target={open ? { mode: 'create' } : null}
        onSubmit={onSubmit}
        onClose={() => {
          setOpen(false);
          setClosed((count) => count + 1);
        }}
      />
    </I18nProvider>
  );
}

test('keeps the MCP draft and blocks closing until a failed save finishes', async () => {
  let rejectSave: (error: Error) => void = () => {};
  render(
    <EditorSample
      onSubmit={() =>
        new Promise<void>((_, reject) => {
          rejectSave = reject;
        })
      }
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Open editor' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'research-docs' } });
  fireEvent.change(within(dialog).getByLabelText('MCP Server URL'), {
    target: { value: 'https://docs.example.test/mcp' },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create', exact: true }));
  expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
  expect(within(dialog).getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(screen.getByText('Closed 0')).toBeTruthy();
  rejectSave(new Error('Sample save failed'));
  expect(await within(dialog).findByRole('alert')).toBeTruthy();
  expect(within(dialog).getByLabelText('Name')).toHaveProperty('value', 'research-docs');
  expect(within(dialog).getByLabelText('MCP Server URL')).toHaveProperty('value', 'https://docs.example.test/mcp');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(screen.getByText('Closed 1')).toBeTruthy();
});

test('cancels an MCP draft without submitting and opens a fresh draft next time', async () => {
  let submitted = 0;
  render(
    <EditorSample
      onSubmit={async () => {
        submitted++;
      }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Open editor' }));
  let dialog = await screen.findByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Discarded' } });
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: 'Open editor' }));
  dialog = await screen.findByRole('dialog');
  expect(within(dialog).getByLabelText('Name')).toHaveProperty('value', '');
  expect(submitted).toBe(0);
});
