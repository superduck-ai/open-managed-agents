import { afterEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { I18nProvider } from '../../shared/i18n';
import { resetTestDom } from '../../test/setup';
import { CreateTunnelDialog } from './components';

const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);

test('keeps tunnel creation open and locks its draft while the request is pending', async () => {
  resetTestDom('https://oma.duck.ai/settings/workspaces/default/mcp-tunnels');
  let closed = 0;
  render(
    <I18nProvider initialLocale="en">
      <CreateTunnelDialog
        open
        busy
        displayName="Local research"
        onOpenChange={() => closed++}
        onOpenChangeComplete={() => {}}
        onDisplayNameChange={() => {}}
        onSubmit={(event) => event.preventDefault()}
      />
    </I18nProvider>,
  );
  const dialog = await screen.findByRole('dialog');
  for (const name of ['Cancel', 'Close', 'Create']) {
    expect(within(dialog).getByRole('button', { name, exact: true }).hasAttribute('disabled')).toBe(true);
  }
  expect(within(dialog).getByRole('textbox', { name: 'Name' }).hasAttribute('disabled')).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(closed).toBe(0);
  expect(screen.getByRole('dialog')).toBe(dialog);
});

function CreationSample() {
  const [open, setOpen] = useState(true);
  const [name, setName] = useState('Local research');
  return (
    <>
      <button onClick={() => setOpen(true)}>Open tunnel creation</button>
      <output data-testid="draft-name">{name}</output>
      <CreateTunnelDialog
        open={open}
        busy={false}
        displayName={name}
        onOpenChange={setOpen}
        onOpenChangeComplete={(nextOpen) => {
          if (!nextOpen && !open) setName('');
        }}
        onDisplayNameChange={setName}
        onSubmit={(event) => event.preventDefault()}
      />
    </>
  );
}

test('clears a cancelled tunnel draft after closing and opens a blank form next time', async () => {
  resetTestDom('https://oma.duck.ai/settings/workspaces/default/mcp-tunnels');
  render(
    <I18nProvider initialLocale="en">
      <CreationSample />
    </I18nProvider>,
  );
  const dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  expect(screen.getByTestId('draft-name').textContent).toBe('Local research');
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  await waitFor(() => expect(screen.getByTestId('draft-name').textContent).toBe(''));
  fireEvent.click(screen.getByRole('button', { name: 'Open tunnel creation' }));
  expect(
    within(await screen.findByRole('dialog'))
      .getByRole('textbox', { name: 'Name' })
      .getAttribute('value'),
  ).toBe('');
});
