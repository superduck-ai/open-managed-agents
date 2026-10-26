import { resetTestDom } from '../../../test/setup';
import { afterEach, describe, expect, test } from 'bun:test';
import { useState } from 'react';
import { I18nProvider } from '../../../shared/i18n';
import { blankAgentTemplate, createDialogAgentConfig } from '../agentConfig';
import { CreateDialogToolsEditor } from './create-dialog-tools-editor';

const { cleanup, fireEvent, render, screen, within } = await import('@testing-library/react');

afterEach(cleanup);

describe('create-agent tool permission selection', () => {
  test.each([
    ['en', 'Always allow', 'Always ask', 'Always deny'],
    ['zh-CN', '始终允许', '始终询问', '始终拒绝'],
  ] as const)('announces the selected permission and keeps other tools unchanged in %s', (locale, allow, ask, deny) => {
    resetTestDom('https://oma.duck.ai/');
    function Sample() {
      const [draft, setDraft] = useState(createDialogAgentConfig(blankAgentTemplate, locale, undefined, 'test-model'));
      return (
        <I18nProvider initialLocale={locale}>
          <CreateDialogToolsEditor
            draft={draft}
            directoryServers={[]}
            directoryLoading={false}
            directoryError={false}
            workspaceServers={[]}
            workspaceServersLoading={false}
            workspaceServersError={false}
            onRetryDirectory={() => {}}
            onRetryWorkspaceServers={() => {}}
            onCreateWorkspaceServer={() => {}}
            onProbeTunnel={async () => []}
            onChange={setDraft}
          />
        </I18nProvider>
      );
    }
    render(<Sample />);
    const question = within(screen.getByText('ask_user_question').parentElement!);
    expect(question.getByRole('button', { name: deny, pressed: true })).toBeTruthy();
    const bash = within(screen.getByText('bash').parentElement!);
    expect(bash.getByRole('button', { name: allow, pressed: true })).toBeTruthy();
    fireEvent.click(bash.getByRole('button', { name: ask }));
    expect(bash.getByRole('button', { name: ask, pressed: true })).toBeTruthy();
    expect(bash.getByRole('button', { name: allow, pressed: false })).toBeTruthy();
    expect(bash.getByRole('button', { name: deny, pressed: false })).toBeTruthy();
    expect(question.getByRole('button', { name: deny, pressed: true })).toBeTruthy();
  });
});
