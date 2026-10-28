import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { useState } from 'react';
import { I18nProvider } from '../../shared/i18n';
import { ProbeDialog } from './components';
import type { McpTunnel, TunnelProbeResult } from './api';

const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);

const tunnel: McpTunnel = {
  id: 'tunl_sample',
  type: 'tunnel',
  display_name: 'Research tools',
  domain: 'example.test',
  created_at: '2026-10-25T00:00:00Z',
  archived_at: null,
  mcp_url: 'https://example.test/mcp',
  connection: { state: 'connected', instance_count: 1, channels: [] },
};
const result: TunnelProbeResult = { status: 'ok', channel: 'research', tools: [{ name: 'search' }] };

function Sample() {
  const [open, setOpen] = useState(false);
  const [closed, setClosed] = useState(0);
  return (
    <I18nProvider initialLocale="en">
      <button onClick={() => setOpen(true)}>View result</button>
      <p role="status">Closed {closed}</p>
      <ProbeDialog
        probe={open ? { tunnel, result } : null}
        onClose={() => {
          setOpen(false);
          setClosed((count) => count + 1);
        }}
      />
    </I18nProvider>
  );
}

for (const dismissal of ['Done', 'Close', 'Escape']) {
  test(`closes a probe result with ${dismissal} once and reopens the result`, async () => {
    render(<Sample />);
    expect(screen.queryByRole('dialog')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'View result' }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText('search')).toBeTruthy();
    if (dismissal === 'Escape') fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
    else fireEvent.click(within(dialog).getByRole('button', { name: dismissal, exact: true }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Closed 1')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'View result' }));
    expect(within(await screen.findByRole('dialog')).getByText('search')).toBeTruthy();
  });
}
