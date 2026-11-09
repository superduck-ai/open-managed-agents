import { afterEach, expect, test } from 'bun:test';
import '../../../test/setup';
import { useState } from 'react';
import { I18nProvider } from '../../../shared/i18n';
import { ConfirmAgentsArchiveDialog, ConfirmEntityDialog } from './common';
import type { MemoryStoreApiResponse } from '../types';
const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);
const entity: MemoryStoreApiResponse = {
  id: 'memstr_sample',
  type: 'memory_store',
  name: 'Research',
  archived_at: null,
  description: '',
  created_at: '2026-10-25T00:00:00Z',
  updated_at: '2026-10-25T00:00:00Z',
};
function Sample({ agents, pending = false }: { agents: boolean; pending?: boolean }) {
  const [open, setOpen] = useState(true);
  const [busy, setBusy] = useState(pending);
  const [closed, setClosed] = useState(0);
  const [confirmed, setConfirmed] = useState(0);
  const props = {
    busy,
    onCancel: () => {
      setOpen(false);
      setClosed((count) => count + 1);
    },
    onConfirm: () => {
      setBusy(true);
      setConfirmed((count) => count + 1);
    },
  };
  return (
    <I18nProvider initialLocale="en">
      <button onClick={() => setOpen(true)}>Open confirmation</button>
      <button onClick={() => setBusy(false)}>Fail request</button>
      <p role="status">
        Closed {closed} Confirmed {confirmed}
      </p>
      {open ? (
        agents ? (
          <ConfirmAgentsArchiveDialog count={3} {...props} />
        ) : (
          <ConfirmEntityDialog action="delete" section="memory-stores" entity={entity} {...props} />
        )
      ) : null}
    </I18nProvider>
  );
}
for (const agents of [false, true]) {
  test(`${agents ? 'Agent archive' : 'Resource delete'} keeps a pending confirmation open and can cancel after failure`, async () => {
    render(<Sample agents={agents} />);
    const dialog = await screen.findByRole('alertdialog');
    fireEvent.click(within(dialog).getByRole('button', { name: agents ? 'Archive' : 'Delete', exact: true }));
    expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
    fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
    expect(screen.getByText('Closed 0 Confirmed 1')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Fail request', hidden: true }));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
    expect(screen.getByText('Closed 1 Confirmed 1')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Open confirmation' }));
    expect(await screen.findByRole('alertdialog')).toBeTruthy();
  });
  test(`${agents ? 'Agent archive' : 'Resource delete'} Escape cancels without confirming`, async () => {
    render(<Sample agents={agents} />);
    const dialog = await screen.findByRole('alertdialog');
    fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
    expect(screen.getByText('Closed 1 Confirmed 0')).toBeTruthy();
  });
}
