import { expect, test } from 'bun:test';
import { consoleNavigation, workspaceManagementNavigation } from './navigation';

test('retains implemented destinations without exposing placeholder pages or duplicate API key entries', () => {
  const destinations = [
    ...consoleNavigation.flatMap((item) =>
      item.type === 'link' ? [item.href] : item.children.map((child) => child.href),
    ),
    ...workspaceManagementNavigation.children.filter((item) => !item.hidden).map((item) => item.href),
  ];
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
      '/evaluations',
      '/observability',
      '/deployments',
      '/environments',
      '/channels',
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

test('keeps placeholder routes defined but out of the workspace directory', () => {
  expect(workspaceManagementNavigation.children.filter((item) => item.hidden).map((item) => item.href)).toEqual([
    '/usage/cache',
    '/usage/limits',
    '/logs',
  ]);
});
