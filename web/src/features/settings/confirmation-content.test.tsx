import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { ArchiveWorkspaceDialog } from '../../shared/workspaces/ArchiveWorkspaceDialog';
import { MemberRemovalDialog } from './MemberRemovalDialog';
const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');
afterEach(cleanup);
for (const workspace of [false, true]) {
  test(`${workspace ? 'workspace archive' : 'member removal'} retains its target and error during exit and updates on reopen`, async () => {
    const view = (open: boolean, name: string, error?: string) => (
      <I18nProvider initialLocale="en">
        {workspace ? (
          <ArchiveWorkspaceDialog
            open={open}
            workspaceName={name}
            pending={false}
            error={error}
            onClose={() => {}}
            onConfirm={() => {}}
          />
        ) : (
          <MemberRemovalDialog
            open={open}
            title="Remove member?"
            pending={false}
            error={error}
            onClose={() => {}}
            onConfirm={() => {}}
          >
            <span>{name}</span>
          </MemberRemovalDialog>
        )}
      </I18nProvider>
    );
    const rendered = render(view(true, 'Research', 'Example failure'));
    await screen.findByRole('alertdialog');
    rendered.rerender(view(false, ''));
    expect(document.querySelector('[data-slot="alert-dialog-description"]')?.textContent).toContain('Research');
    expect(document.querySelector('[role="alert"]')?.textContent).toContain('Example failure');
    rendered.rerender(view(true, 'Operations'));
    expect(screen.getByRole('alertdialog').textContent).toContain('Operations');
    expect(screen.getByRole('alertdialog').textContent).not.toContain('Research');
    expect(screen.queryByRole('alert')).toBeNull();
  });
  test(`${workspace ? 'workspace archive' : 'member removal'} rejects Escape while pending and cancels once after failure`, async () => {
    let closed = 0;
    const onClose = () => {
      closed++;
    };
    const view = (pending: boolean) => (
      <I18nProvider initialLocale="en">
        {workspace ? (
          <ArchiveWorkspaceDialog
            open
            workspaceName="Research"
            pending={pending}
            onClose={onClose}
            onConfirm={() => {}}
          />
        ) : (
          <MemberRemovalDialog open title="Remove member?" pending={pending} onClose={onClose} onConfirm={() => {}}>
            Research
          </MemberRemovalDialog>
        )}
      </I18nProvider>
    );
    const rendered = render(view(true));
    fireEvent.keyDown(await screen.findByRole('alertdialog'), { key: 'Escape', code: 'Escape' });
    expect(closed).toBe(0);
    rendered.rerender(view(false));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(closed).toBe(1);
  });
}
