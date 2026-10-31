import { afterEach, expect, test } from 'bun:test';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { BuilderPanels } from './BuilderPanels';
import { useBuilderPanel } from './panelContext';
import { newBuilderState } from './model';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import type { Builder } from './useBuilder';

function OpenConfiguration() {
  const open = useBuilderPanel();
  return <button onClick={() => open({ kind: 'agent' })}>Open config</button>;
}

function builderFixture(sessionID?: string): Builder {
  return {
    state: {
      ...newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'claude-sonnet-4-6')),
      sessionID,
    },
    busy: false,
    draftEditing: false,
    setDraftEditing: () => {},
  } as Builder;
}

function view(builder: Builder) {
  return (
    <BuilderPanels builder={builder} workspaceId="default">
      <OpenConfiguration />
    </BuilderPanels>
  );
}

afterEach(cleanup);

test('entering a test closes configuration but ordinary session updates do not close a reopened panel', async () => {
  const builder = builderFixture();
  const page = render(view(builder));
  fireEvent.click(screen.getByRole('button', { name: 'Open config' }));
  expect(screen.getByRole('button', { name: 'Close configuration panel' })).toBeTruthy();
  page.rerender(view(builderFixture('session-1')));
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Close configuration panel' })).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: 'Open config' }));
  page.rerender(view(builderFixture('session-1')));
  expect(screen.getByRole('button', { name: 'Close configuration panel' })).toBeTruthy();
  page.rerender(view(builderFixture('session-2')));
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Close configuration panel' })).toBeNull());
});

test('close control hides the configuration and restores the conversation', () => {
  render(view(builderFixture()));
  fireEvent.click(screen.getByRole('button', { name: 'Open config' }));
  expect(screen.queryByRole('button', { name: 'Open config' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Close configuration panel' }));
  expect(screen.getByRole('button', { name: 'Open config' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Close configuration panel' })).toBeNull();
});
