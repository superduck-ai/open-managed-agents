import '../../test/setup';
import { afterEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { I18nProvider } from '../../shared/i18n';
import { WorkbenchDrawer } from './dialogs';

const { act, cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);

function DrawerSample() {
  const [mounted, setMounted] = useState(false);
  const [open, setOpen] = useState(false);
  const [closed, setClosed] = useState(0);
  const [reason, setReason] = useState('');
  return (
    <I18nProvider initialLocale="en">
      <button
        onClick={() => {
          setMounted(true);
          setOpen(true);
        }}
      >
        Open drawer
      </button>
      <button onClick={() => setOpen(false)}>Close programmatically</button>
      <button>Outside</button>
      <output>{`Closed ${closed} Reason ${reason}`}</output>
      {mounted ? (
        <WorkbenchDrawer
          open={open}
          title="Model"
          kind="model"
          onClose={(nextReason) => {
            setReason(nextReason ?? 'close');
            setOpen(false);
          }}
          onClosed={() => {
            setMounted(false);
            setClosed((value) => value + 1);
          }}
        >
          <input aria-label="Draft" defaultValue="Keep this draft during exit" />
        </WorkbenchDrawer>
      ) : null}
    </I18nProvider>
  );
}

for (const action of ['button', 'escape', 'programmatic']) {
  test(`retains drawer content until the ${action} exit completes`, async () => {
    render(<DrawerSample />);
    const opener = screen.getByRole('button', { name: 'Open drawer' });
    opener.focus();
    fireEvent.click(opener);
    const panel = await screen.findByRole('dialog', { name: 'Model' });
    const draft = within(panel).getByRole('textbox', { name: 'Draft' });
    let finishExit = () => {};
    const exit = new Promise<void>((resolve) => {
      finishExit = resolve;
    });
    Object.defineProperty(panel, 'getAnimations', {
      configurable: true,
      value: () => (panel.hasAttribute('data-ending-style') ? [{ finished: exit }] : []),
    });
    draft.focus();
    if (action === 'button') fireEvent.click(within(panel).getByRole('button', { name: 'Close' }));
    if (action === 'escape') fireEvent.keyDown(draft, { key: 'Escape', code: 'Escape' });
    if (action === 'programmatic') fireEvent.click(screen.getByRole('button', { name: 'Close programmatically' }));
    await waitFor(() => expect(panel.hasAttribute('data-ending-style')).toBe(true));
    expect(panel.isConnected).toBe(true);
    expect(draft).toHaveProperty('value', 'Keep this draft during exit');
    expect(screen.getByText(/Closed 0/)).toBeTruthy();
    await act(async () => finishExit());
    await waitFor(() => expect(panel.isConnected).toBe(false));
    expect(screen.getByText(/Closed 1/)).toBeTruthy();
    if (action === 'escape') expect(screen.getByText(/Reason escape/)).toBeTruthy();
    expect(document.activeElement).toBe(opener);
  });
}

test('keeps a nonmodal drawer open while interacting with the background', async () => {
  render(<DrawerSample />);
  fireEvent.click(screen.getByRole('button', { name: 'Open drawer' }));
  const panel = await screen.findByRole('dialog', { name: 'Model' });
  expect(panel.getAttribute('aria-modal')).not.toBe('true');
  expect(document.querySelector('[data-slot="sheet-overlay"]')).toBeNull();
  const outside = screen.getByRole('button', { name: 'Outside' });
  await act(async () => {
    outside.focus();
    fireEvent.pointerDown(outside, { button: 0 });
    fireEvent.click(outside);
  });
  expect(panel.hasAttribute('data-open')).toBe(true);
  expect(document.activeElement).toBe(outside);
});
