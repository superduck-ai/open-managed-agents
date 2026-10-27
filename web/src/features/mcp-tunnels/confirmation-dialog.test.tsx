import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { ConfirmTunnelActionDialog, type PendingTunnelAction } from './components';
import type { McpTunnel } from './api';
const { cleanup, fireEvent, render, screen, within } = await import('@testing-library/react');
afterEach(cleanup);
const tunnel: McpTunnel = {
  id: 'tunl_sample',
  type: 'tunnel',
  display_name: 'Research',
  domain: 'example.test',
  created_at: '2026-10-25T00:00:00Z',
  archived_at: null,
  mcp_url: 'https://example.test/mcp',
  connection: { state: 'connected', instance_count: 1, channels: [] },
};

test('retains rotation content when its owner clears selection and updates for the next action', async () => {
  const view = (action: PendingTunnelAction | null) => (
    <I18nProvider initialLocale="en">
      <ConfirmTunnelActionDialog action={action} busy={false} onClose={() => {}} onConfirm={() => {}} />
    </I18nProvider>
  );
  const rendered = render(view({ type: 'rotate', tunnel }));
  await screen.findByRole('alertdialog');
  rendered.rerender(view(null));
  expect(document.querySelector('[data-slot="alert-dialog-title"]')?.textContent).toBe('Rotate tunnel token?');
  expect(document.querySelector('[data-slot="alert-dialog-description"]')?.textContent).toContain(
    'current tunnel-client',
  );
  rendered.rerender(view({ type: 'archive', tunnel }));
  expect(within(await screen.findByRole('alertdialog')).getByRole('heading').textContent).toBe('Archive MCP tunnel?');
});

test('rejects Escape while a tunnel action is pending', async () => {
  let closed = 0;
  render(
    <I18nProvider initialLocale="en">
      <ConfirmTunnelActionDialog
        action={{ type: 'rotate', tunnel }}
        busy
        onClose={() => {
          closed++;
        }}
        onConfirm={() => {}}
      />
    </I18nProvider>,
  );
  const dialog = await screen.findByRole('alertdialog');
  expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(closed).toBe(0);
  expect(screen.getByRole('alertdialog')).toBe(dialog);
});
