import { expect, test } from 'bun:test';
import { quickstartModelSetupHref, quickstartModelSetupReturn } from './model-setup-navigation';

test.each([
  'https://evil.test',
  '//evil.test/path',
  '/workspaces/other/agent-quickstart',
  '/settings/profile',
  '/\\evil.test',
])('rejects unexpected return destination %s', (returnTo) => {
  expect(quickstartModelSetupReturn(`?returnTo=${encodeURIComponent(returnTo)}`, 'team')).toBeNull();
});

test.each(['api', 'chat'])('preserves the workspace and %s creation mode', (mode) => {
  const path = quickstartModelSetupHref('team / 1', `?mode=${mode}`);
  expect(path.startsWith('/workspaces/team%20%2F%201/llm-models?')).toBe(true);
  expect(quickstartModelSetupReturn(new URL(path, 'https://oma.local').search, 'team / 1')).toBe(
    `/workspaces/team%20%2F%201/agent-quickstart?mode=${mode}`,
  );
});

test('ordinary model management has no quickstart return', () => {
  expect(quickstartModelSetupReturn('', 'team')).toBeNull();
});
