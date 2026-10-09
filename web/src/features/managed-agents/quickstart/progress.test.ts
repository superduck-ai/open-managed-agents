import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../ManagedAgentsPage.test-utils';
import { quickstartDraft } from './model';
import { initialQuickstartProgress, loadQuickstartProgress, storeQuickstartProgress } from './progress';

const storageKey = 'quickstart-recovery-test';

afterEach(() => window.sessionStorage.clear());

test('legacy saved progress without an uncertain request is discarded on load', () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const saved = initialQuickstartProgress(quickstartDraft('hello', 'en', 'test-model'));
  window.sessionStorage.setItem(storageKey, JSON.stringify({ ...saved, step: 3, sessionID: 'session_old' }));
  expect(loadQuickstartProgress(storageKey)).toBeNull();
  expect(window.sessionStorage.getItem(storageKey)).toBeNull();
});

test.each(['agent', 'environment', 'session'] as const)(
  'an uncertain %s create survives until its result is confirmed',
  (kind) => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    const saved = initialQuickstartProgress(quickstartDraft('hello', 'en', 'test-model'));
    const pending = { kind, id: 'operation_uncertain' };
    expect(storeQuickstartProgress(storageKey, { ...saved, pending })).toBe(true);
    expect(loadQuickstartProgress(storageKey)?.pending).toEqual(pending);
    expect(storeQuickstartProgress(storageKey, saved)).toBe(true);
    expect(loadQuickstartProgress(storageKey)).toBeNull();
  },
);

test('an unconfirmed message retains its Session binding until explicitly resolved', () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const saved = initialQuickstartProgress(quickstartDraft('hello', 'en', 'test-model'));
  const recovery = { ...saved, sessionID: 'session_uncertain', sessionBinding: 'binding', unconfirmedMessage: 'Hello' };
  expect(storeQuickstartProgress(storageKey, recovery)).toBe(true);
  expect(loadQuickstartProgress(storageKey)).toMatchObject(recovery);
  expect(storeQuickstartProgress(storageKey, { ...recovery, unconfirmedMessage: '' })).toBe(true);
  expect(loadQuickstartProgress(storageKey)).toBeNull();
});
