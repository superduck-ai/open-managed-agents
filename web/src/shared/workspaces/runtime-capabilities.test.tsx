import { afterEach, expect, mock, test } from 'bun:test';
import '../../test/setup';
import { I18nProvider } from '../i18n';
import { CreateWorkspaceDialog } from './CreateWorkspaceDialog';
import { WorkspaceContext, useWorkspaceCapabilities, useWorkspaceRuntime } from './context';
import { defaultWorkspace } from './api';
import { defaultNativeCapabilities, type RuntimeCapabilities, type WorkspaceRuntimeSummary } from './runtime';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

const qoderCapabilities: RuntimeCapabilities = {
  mcp_tunnels: false,
  port_preview: false,
  otlp_traces: false,
  observability_dashboard: false,
  byok_llm_providers: false,
  deployments: false,
  session_webhooks: false,
  session_budget: 'credits',
  session_agent_overrides: false,
  channels: false,
  evaluations: false,
  skills: false,
  memory_stores: false,
  credential_vaults: false,
  mcp_servers: false,
  usage_metering: 'provider_credits',
  artifact_discovery: 'explicit_deliver_tool',
  file_constraints: {
    max_size_bytes: 5 * 1024 * 1024,
    allowed_mime_prefixes: ['text/', 'image/'],
    allow_binary: false,
  },
};

function Probe() {
  const runtime = useWorkspaceRuntime();
  const caps = useWorkspaceCapabilities();
  return (
    <div>
      <span data-testid="provider">{runtime.provider_kind}</span>
      <span data-testid="name">{runtime.profile_name}</span>
      <span data-testid="deployments">{String(caps.deployments)}</span>
      <span data-testid="byok">{String(caps.byok_llm_providers)}</span>
      <span data-testid="budget">{caps.session_budget}</span>
      <span data-testid="artifact">{caps.artifact_discovery}</span>
    </div>
  );
}

test('falls back to full oma_native capabilities when workspace.runtime is missing', () => {
  render(
    <WorkspaceContext.Provider
      value={{
        orgUuid: 'org-1',
        workspaces: [defaultWorkspace],
        activeWorkspace: defaultWorkspace,
        activeWorkspaceId: defaultWorkspace.id,
        isLoading: false,
        error: null,
        selectWorkspace: () => undefined,
        createWorkspace: async () => defaultWorkspace,
        refreshWorkspaces: async () => undefined,
      }}
    >
      <Probe />
    </WorkspaceContext.Provider>,
  );

  expect(screen.getByTestId('provider').textContent).toBe('oma_native');
  expect(screen.getByTestId('deployments').textContent).toBe(String(defaultNativeCapabilities.deployments));
  expect(screen.getByTestId('byok').textContent).toBe('true');
  expect(screen.getByTestId('budget').textContent).toBe('usd');
  expect(screen.getByTestId('artifact').textContent).toBe('filesystem_outputs_dir');
});

test('exposes external workspace runtime and capability matrix from WorkspaceContext', () => {
  const runtime: WorkspaceRuntimeSummary = {
    profile_uuid: '00000000-0000-4000-8000-000000000099',
    profile_id: 'rprof_qoder_cn',
    profile_name: 'Qoder Cloud CN',
    provider_kind: 'qoder_cloud',
    capabilities: qoderCapabilities,
  };
  const workspace = { ...defaultWorkspace, id: 'wrkspc_qoder', name: 'Qoder Lab', runtime };

  render(
    <WorkspaceContext.Provider
      value={{
        orgUuid: 'org-1',
        workspaces: [workspace],
        activeWorkspace: workspace,
        activeWorkspaceId: workspace.id,
        isLoading: false,
        error: null,
        selectWorkspace: () => undefined,
        createWorkspace: async () => workspace,
        refreshWorkspaces: async () => undefined,
      }}
    >
      <Probe />
    </WorkspaceContext.Provider>,
  );

  expect(screen.getByTestId('provider').textContent).toBe('qoder_cloud');
  expect(screen.getByTestId('name').textContent).toBe('Qoder Cloud CN');
  expect(screen.getByTestId('deployments').textContent).toBe('false');
  expect(screen.getByTestId('byok').textContent).toBe('false');
  expect(screen.getByTestId('budget').textContent).toBe('credits');
  expect(screen.getByTestId('artifact').textContent).toBe('explicit_deliver_tool');
});

test('CreateWorkspaceDialog shows runtime engine selector when organization has multiple profiles and submits selected id', async () => {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    if (String(input).includes('/runtime-profiles')) {
      return Response.json({
        default_runtime_profile_id: 'rprof_qoder_cn',
        profiles: [
          {
            uuid: '00000000-0000-4000-8000-000000000001',
            id: 'rprof_oma_native_default',
            name: 'OMA Native',
            provider_kind: 'oma_native',
            status: 'active',
          },
          {
            uuid: '00000000-0000-4000-8000-000000000099',
            id: 'rprof_qoder_cn',
            name: 'Qoder Cloud CN',
            provider_kind: 'qoder_cloud',
            status: 'active',
            is_default: true,
          },
        ],
      });
    }
    return Response.json({});
  }) as typeof fetch;

  const create = mock(async () => undefined);
  render(
    <I18nProvider initialLocale="zh-CN">
      <CreateWorkspaceDialog open orgUuid="org-1" onOpenChange={() => undefined} onCreate={create} />
    </I18nProvider>,
  );

  expect(await screen.findByText('运行时引擎')).toBeTruthy();
  expect(screen.getByText(/创建后不可更改/)).toBeTruthy();

  fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'Qoder Team' } });
  fireEvent.click(screen.getByRole('button', { name: '创建' }));

  await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
  expect(create).toHaveBeenCalledWith('Qoder Team', '#E8A8C9', 'rprof_qoder_cn');
});

test('CreateWorkspaceDialog hides runtime engine selector when organization only has one profile', async () => {
  globalThis.fetch = mock(async () =>
    Response.json([
      {
        uuid: '00000000-0000-4000-8000-000000000001',
        id: 'rprof_oma_native_default',
        name: 'OMA Native',
        provider_kind: 'oma_native',
        status: 'active',
        is_default: true,
      },
    ]),
  ) as typeof fetch;

  const create = mock(async () => undefined);
  render(
    <I18nProvider initialLocale="zh-CN">
      <CreateWorkspaceDialog open orgUuid="org-1" onOpenChange={() => undefined} onCreate={create} />
    </I18nProvider>,
  );

  fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'Native Only' } });
  fireEvent.click(screen.getByRole('button', { name: '创建' }));

  await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
  expect(screen.queryByText('运行时引擎')).toBeNull();
  expect(create).toHaveBeenCalledWith('Native Only', '#E8A8C9');
});
