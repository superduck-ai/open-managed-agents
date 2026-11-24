import '../../test/setup';
import { afterEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { I18nProvider } from '../../shared/i18n';
import { WorkbenchDrawer } from './dialogs';
import { ModelDrawer, ToolsDrawer } from './drawers';
import { createDefaultRevision, defaultWebSearchToolForm } from './model';

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

test('allows clearing and typing multi-digit values in Workbench numeric settings before clamping on blur', () => {
  function ModelNumericHarness() {
    const [draft, setDraft] = useState(() => ({
      ...createDefaultRevision(),
      model_name: 'claude-sonnet-4-5',
      max_tokens_to_sample: 4096,
      thinking: { type: 'enabled' as const, effort: 'high' as const, budget_tokens: 16000 },
    }));
    return (
      <I18nProvider initialLocale="en">
        <ModelDrawer draft={draft} models={[]} setDraft={setDraft} onRun={() => {}} isRunning={false} />
      </I18nProvider>
    );
  }

  render(<ModelNumericHarness />);
  const maxTokens = screen.getByRole('spinbutton', { name: 'Max tokens' }) as HTMLInputElement;
  fireEvent.change(maxTokens, { target: { value: '' } });
  expect(maxTokens.value).toBe('');
  fireEvent.change(maxTokens, { target: { value: '2048' } });
  expect(maxTokens.value).toBe('2048');
  fireEvent.change(maxTokens, { target: { value: '' } });
  fireEvent.blur(maxTokens);
  expect(maxTokens.value).toBe('1');

  const budgetTokens = screen.getByRole('spinbutton', { name: 'Budget tokens' }) as HTMLInputElement;
  fireEvent.change(budgetTokens, { target: { value: '' } });
  expect(budgetTokens.value).toBe('');
  fireEvent.change(budgetTokens, { target: { value: '2' } });
  expect(budgetTokens.value).toBe('2');
  fireEvent.change(budgetTokens, { target: { value: '204' } });
  expect(budgetTokens.value).toBe('204');
  fireEvent.change(budgetTokens, { target: { value: '2048' } });
  expect(budgetTokens.value).toBe('2048');
  fireEvent.change(budgetTokens, { target: { value: '500' } });
  fireEvent.blur(budgetTokens);
  expect(budgetTokens.value).toBe('1024');
});

test('allows clearing and replacing Web search max uses before normalizing on blur', () => {
  function WebSearchNumericHarness() {
    const [toolForm, setToolForm] = useState<'custom' | 'web_search' | null>('web_search');
    const [customTool, setCustomTool] = useState(() => ({ name: '', description: '', schema: '{}' }));
    const [webSearchTool, setWebSearchTool] = useState(() => ({
      ...defaultWebSearchToolForm(),
      maxUsesEnabled: true,
      maxUses: 5,
    }));
    return (
      <I18nProvider initialLocale="en">
        <ToolsDrawer
          tools={[]}
          toolForm={toolForm}
          setToolForm={setToolForm}
          customTool={customTool}
          setCustomTool={setCustomTool}
          webSearchTool={webSearchTool}
          setWebSearchTool={setWebSearchTool}
          onAddCustom={() => {}}
          onAddWebSearch={() => {}}
          onUpdateWebSearch={() => {}}
          onRemove={() => {}}
          onRun={() => {}}
          canAddCustom={false}
          canAddWebSearch
          isRunning={false}
        />
      </I18nProvider>
    );
  }

  render(<WebSearchNumericHarness />);
  const maxUses = screen.getByLabelText('Web search max uses') as HTMLInputElement;
  expect(maxUses.value).toBe('5');
  fireEvent.change(maxUses, { target: { value: '' } });
  expect(maxUses.value).toBe('');
  fireEvent.change(maxUses, { target: { value: '8' } });
  expect(maxUses.value).toBe('8');
  fireEvent.change(maxUses, { target: { value: '' } });
  fireEvent.blur(maxUses);
  expect(maxUses.value).toBe('1');
});
