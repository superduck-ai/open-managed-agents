import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { useState } from 'react';
import {
  Dialog,
  DeletePromptDialog,
  SharePromptDialog,
  GeneratePromptDialog,
  PromptGeneratorConfirmDialog,
} from './dialogs';
import { I18nProvider } from '../../shared/i18n';
import { AttachmentUrlDialog, MessageEditorPlaceholder } from './editor';
import { workbenchPromptGeneratorWarning } from './model';
import { DialogClose } from '../../shared/ui/dialog';
import { Button } from '../../shared/ui/button';
const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);

for (const [locale, expected] of [
  ['en', 'Oct 25, 2026 by you'],
  ['zh-CN', '2026年10月25日，由你创建'],
] as const) {
  test(`sharing dates follow the selected ${locale} locale`, async () => {
    render(
      <I18nProvider initialLocale={locale}>
        <SharePromptDialog
          prompt={{ id: 'prompt_sample', name: 'Research', created_at: '2026-10-25T00:00:00Z' }}
          promptTitle="Research"
          account={null}
          isSharing={false}
          error={null}
          onShare={() => {}}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    expect(within(await screen.findByRole('dialog')).getByText(expected)).toBeTruthy();
  });
}
test('preserves an actual creator named you while localizing read-only sharing guidance', async () => {
  render(
    <I18nProvider initialLocale="zh-CN">
      <SharePromptDialog
        prompt={{ id: 'prompt_named_creator', name: 'Original title', creator: { full_name: 'you' } }}
        promptTitle="Original title"
        account={null}
        isSharing={false}
        error={null}
        onShare={() => {}}
        onClose={() => {}}
      />
    </I18nProvider>,
  );
  const dialog = await screen.findByRole('dialog', { name: '分享提示词' });
  expect(within(dialog).getByText('由you创建')).toBeTruthy();
  expect(within(dialog).getByText('联系创建者 you 修改共享设置。')).toBeTruthy();
  expect(within(dialog).getByText('Original title')).toBeTruthy();
  expect(within(dialog).queryByRole('button', { name: '分享到工作区' })).toBeNull();
});

const generatorProps = {
  step: 'output' as const,
  task: 'Research summary',
  setTask: () => {},
  output: 'Partial draft',
  setOutput: () => {},
  examplesExpanded: false,
  setExamplesExpanded: () => {},
  thinkingEnabled: false,
  setThinkingEnabled: () => {},
  warning: null,
  error: null,
  isGenerating: true,
  onGenerate: () => {},
  onStop: () => {},
  onEdit: () => {},
  onOpen: () => {},
  onSelectExample: () => {},
  onBuyCredits: () => {},
};

function Sample({
  pending = false,
  guarded = false,
  attachment = false,
  initiallyOpen = true,
}: {
  pending?: boolean;
  guarded?: boolean;
  attachment?: boolean;
  initiallyOpen?: boolean;
}) {
  const [opened, setOpened] = useState(initiallyOpen);
  const [closed, setClosed] = useState(0);
  const [requests, setRequests] = useState(0);
  const [url, setUrl] = useState('https://example.test/document.pdf');
  const close = () => {
    setOpened(false);
    setClosed((count) => count + 1);
  };
  return (
    <>
      <button onClick={() => setOpened(true)}>Open sample</button>
      <p role="status">
        Closed {closed} Requests {requests}
      </p>
      {opened ? (
        attachment ? (
          <AttachmentUrlDialog kind="pdf" url={url} setUrl={setUrl} onSubmit={() => {}} onClose={close} />
        ) : guarded ? (
          <GeneratePromptDialog
            {...generatorProps}
            open
            onExited={() => {}}
            onClose={() => setRequests((count) => count + 1)}
          />
        ) : (
          <Dialog title="Sample dialog" closeDisabled={pending} onClose={close}>
            <DialogClose render={<Button>Cancel</Button>} />
          </Dialog>
        )
      ) : null}
    </>
  );
}
test('generator leaves the close decision to its controlled owner', async () => {
  render(<Sample guarded />);
  const dialog = await screen.findByRole('dialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
  expect(screen.getByText('Closed 0 Requests 1')).toBeTruthy();
  expect(screen.getByRole('dialog')).toBe(dialog);
});
test('disables the close control and rejects Escape while pending', async () => {
  const view = render(<Sample pending />);
  const dialog = await screen.findByRole('dialog');
  expect(within(dialog).getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(screen.getByText('Closed 0 Requests 0')).toBeTruthy();
  view.rerender(<Sample />);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(screen.getByText('Closed 1 Requests 0')).toBeTruthy();
});
test('attachment Cancel closes once without submitting and can reopen', async () => {
  render(<Sample attachment initiallyOpen={false} />);
  const opener = screen.getByRole('button', { name: 'Open sample' });
  opener.focus();
  fireEvent.click(opener);
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('textbox')));
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(screen.getByText('Closed 1 Requests 0')).toBeTruthy();
  expect(document.activeElement).toBe(opener);
  fireEvent.click(screen.getByRole('button', { name: 'Open sample' }));
  expect(within(await screen.findByRole('dialog')).getByRole('textbox')).toHaveProperty(
    'value',
    'https://example.test/document.pdf',
  );
});
test('delete confirmation rejects pending dismissal and cancels without deleting after failure', async () => {
  let closed = 0;
  let deleted = 0;
  const view = (pending: boolean) => (
    <DeletePromptDialog
      isDeleting={pending}
      onCancel={() => {
        closed++;
      }}
      onDelete={() => {
        deleted++;
      }}
    />
  );
  const rendered = render(view(true));
  const dialog = await screen.findByRole('alertdialog');
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(closed).toBe(0);
  rendered.rerender(view(false));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(closed).toBe(1));
  expect(deleted).toBe(0);
});

function GeneratorExitSample({ controller }: { controller: AbortController }) {
  const [open, setOpen] = useState(true);
  const [running, setRunning] = useState(true);
  const [kind, setKind] = useState<'close' | 'edit' | null>(null);
  const [output, setOutput] = useState('Partial draft');
  const [task, setTask] = useState('Research summary');
  const [step, setStep] = useState<'generate' | 'output'>('output');
  const [exits, setExits] = useState(0);
  const stopAndClose = () => {
    controller.abort();
    setRunning(false);
    setKind(null);
    setOpen(false);
  };
  return (
    <>
      <button onClick={() => setOpen(true)}>Reopen generator</button>
      <p role="status">Exited {exits}</p>
      <GeneratePromptDialog
        {...generatorProps}
        open={open}
        step={step}
        task={task}
        output={output}
        isGenerating={running}
        onClose={() => (running ? setKind('close') : stopAndClose())}
        onExited={() => {
          if (!open) {
            setTask('');
            setOutput('');
            setStep('generate');
            setExits((count) => count + 1);
          }
        }}
      />
      <PromptGeneratorConfirmDialog kind={kind} onCancel={() => setKind(null)} onConfirm={stopAndClose} />
    </>
  );
}

test('confirmed generator close aborts immediately, retains output during exit and resets before reopening', async () => {
  const controller = new AbortController();
  render(<GeneratorExitSample controller={controller} />);
  fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Close', exact: true }));
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  expect(controller.signal.aborted).toBe(false);
  expect(screen.getByRole('textbox', { name: 'Your prompt' })).toHaveProperty('value', 'Partial draft');
  fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Close', exact: true }));
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Close', exact: true }));
  expect(controller.signal.aborted).toBe(true);
  expect(document.querySelector<HTMLTextAreaElement>('textarea[aria-label="Your prompt"]')?.value).toBe(
    'Partial draft',
  );
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(screen.getByText('Exited 1')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Reopen generator' }));
  expect(await screen.findByRole('textbox', { name: 'Describe your task...' })).toHaveProperty('value', '');
});

test('preserves raw generator warnings and output while hiding the empty variable section', async () => {
  const sample = (
    step: 'generate' | 'output',
    output: string,
    warning: { title: string } | null,
    error: string | null,
  ) => (
    <I18nProvider initialLocale="zh-CN">
      <GeneratePromptDialog
        {...generatorProps}
        open
        onExited={() => {}}
        step={step}
        output={output}
        isGenerating={false}
        warning={warning}
        error={error}
      />
    </I18nProvider>
  );
  const view = render(sample('generate', '', { title: 'Raw provider gate' }, 'Raw provider error'));
  expect(screen.getAllByText('Raw provider gate')).toHaveLength(1);
  expect(screen.queryByRole('button', { name: '充值' })).toBeNull();
  expect(screen.getByText('Raw provider error')).toBeTruthy();
  expect(screen.queryByText('Buy credits to use the prompt generator')).toBeNull();
  view.rerender(sample('output', 'Keep this output unchanged.', null, null));
  expect(screen.getByRole('textbox', { name: '生成结果' })).toHaveProperty('value', 'Keep this output unchanged.');
  expect(screen.queryByRole('heading', { name: '变量' })).toBeNull();
  view.rerender(sample('output', 'Keep {{customer_id}} unchanged.', null, null));
  expect(screen.getByLabelText('生成结果中的变量').textContent).toBe('{{customer_id}}');
  expect(screen.getByRole('textbox', { name: '生成结果' })).toHaveProperty('value', 'Keep {{customer_id}} unchanged.');
});

for (const [reason, expected, purchase] of [
  ['policy_restriction', '当前组织的 API 不可用。', false],
  ['out_of_credits', '余额不足，充值后可使用提示词生成器。', true],
] as const) {
  test(`localizes the generator gate and only offers credits for ${reason}`, async () => {
    const account = {
      uuid: 'user_fixture',
      email_address: 'fixture@example.test',
      memberships: [{ organization: { uuid: 'org_fixture', api_disabled_reason: reason } }],
    };
    const warning = workbenchPromptGeneratorWarning(account, 'org_fixture', 0);
    const view = render(
      <I18nProvider initialLocale="zh-CN">
        <MessageEditorPlaceholder
          placeholder=""
          showGeneratePrompt
          warning={warning}
          isReadOnly={false}
          isGenerating={false}
        />
      </I18nProvider>,
    );
    expect(screen.getByRole('button', { name: '生成提示词' }).closest('[title]')?.getAttribute('title')).toBe(expected);
    view.rerender(
      <I18nProvider initialLocale="zh-CN">
        <GeneratePromptDialog
          {...generatorProps}
          open
          onExited={() => {}}
          step="generate"
          output=""
          isGenerating={false}
          warning={warning}
          onClose={() => {}}
        />
      </I18nProvider>,
    );
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(expected)).toBeTruthy();
    expect(Boolean(within(dialog).queryByRole('button', { name: '充值' }))).toBe(purchase);
    expect(within(dialog).getByRole('button', { name: '生成', exact: true }).hasAttribute('disabled')).toBe(true);
  });
}
