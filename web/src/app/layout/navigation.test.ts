import { expect, test } from 'bun:test';
import { consoleNavigation, visibleConsoleNavigation } from './navigation';

test('retains implemented destinations without exposing placeholder pages or duplicate API key entries', () => {
  const destinations = visibleConsoleNavigation().flatMap((item) =>
    item.type === 'link' ? [item.href] : item.children.map((child) => child.href),
  );
  expect(destinations.toSorted()).toEqual(
    [
      '/dashboard',
      '/llm-models',
      '/api-keys',
      '/files',
      '/skills',
      '/mcp-servers',
      '/quickstart',
      '/agents',
      '/sessions',
      '/observability',
      '/deployments',
      '/environments',
      '/credential-vaults',
      '/memory-stores',
      '/usage',
      '/cost',
      '/members',
      '/webhooks',
      '/telemetry',
      '/mcp-tunnels',
    ].toSorted(),
  );
});

test('keeps the original hidden routes defined without exposing them', () => {
  const resources = consoleNavigation.find((item) => item.label === 'Resources');
  expect(
    resources?.type === 'group' && resources.children.filter((child) => child.hidden).map((child) => child.href),
  ).toEqual(['/workbench', '/batches']);
  expect(consoleNavigation.find((item) => item.label === 'Claude Code')).toMatchObject({ hidden: true });
});

test('keeps placeholder routes defined but out of the workspace directory', () => {
  const management = consoleNavigation.find((item) => item.label === 'Workspace management');
  expect(
    management?.type === 'group' && management.children.filter((item) => item.hidden).map((item) => item.href),
  ).toEqual(['/usage/cache', '/usage/limits', '/logs']);
});
