import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, mock, test } from "bun:test";
import { useMemo, type ReactNode } from "react";
import { resetTestDom } from "../../test/setup";
import { ConsoleShell } from "../../app/layout/ConsoleLayout";
import { AuthContext, type AuthContextValue } from "../../shared/auth/context";
import { defaultWorkspace, type WorkspaceApiKey } from "../../shared/workspaces/api";
import { WorkspaceContext, type WorkspaceContextValue } from "../../shared/workspaces/context";
import { WorkspaceApiKeysContent } from "./WorkspaceApiKeysPage";

const testingLibrary = await import("@testing-library/react");
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

const originalFetch = globalThis.fetch;
const originalClipboardDescriptor = Object.getOwnPropertyDescriptor(globalThis.navigator, "clipboard");
const originalExecCommand = document.execCommand;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  restoreClipboard();
  restoreExecCommand();
});

describe("Workspace API keys page", () => {
  test("renders the wide workspace-scoped API keys shell with the workspace switcher", async () => {
    resetTestDom("https://oma.duck.ai/settings/workspaces/default/keys");
    mockWorkspaceApiKeys([activeKey, archivedKey]);

    render(
      <WorkspaceApiKeysHarness>
        <ConsoleShell
          currentPath="/settings/workspaces/default/keys"
          account={{ uuid: "acct_test", email_address: "test@example.com", display_name: "test" }}
          onLogout={() => undefined}
        >
          <WorkspaceApiKeysContent routeWorkspaceId="default" />
        </ConsoleShell>
      </WorkspaceApiKeysHarness>,
    );

    expect(
      screen
        .getAllByRole("button", { name: /Default/i })
        .some((button) => button.getAttribute("aria-label") === "Default"),
    ).toBe(true);
    expect(screen.getByRole("heading", { name: "API keys" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Create key" })).toBeTruthy();
    expect(screen.getByText(/API keys are owned by workspaces/i)).toBeTruthy();
    expect(screen.getByText("Created by")).toBeTruthy();
    expect(screen.getByText("Created at")).toBeTruthy();
    expect(screen.getByText("Last used at")).toBeTruthy();
    expect(screen.getAllByText("Cost").length).toBeGreaterThan(0);
    expect(screen.getByText("Actions")).toBeTruthy();

    await screen.findByText("foo");
    expect(screen.queryByText("archived")).toBeNull();
    expect(screen.getByTestId("workspace-api-keys-page").className).toContain("max-w-none");
    expect(screen.getByTestId("workspace-api-keys-page").parentElement?.className).toContain("lg:px-8");
  });

  test("creates an API key, posts the official body, refreshes, and shows raw key once", async () => {
    resetTestDom("https://oma.duck.ai/settings/workspaces/default/keys");
    const api = mockWorkspaceApiKeys([activeKey]);

    render(
      <WorkspaceApiKeysHarness>
        <WorkspaceApiKeysContent routeWorkspaceId="default" />
      </WorkspaceApiKeysHarness>,
    );

    await screen.findByText("foo");
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));

    expect(screen.getByRole("dialog", { name: "Create API key" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add" }).hasAttribute("disabled")).toBe(true);
    expect(screen.getByText("Workspace")).toBeTruthy();
    expect(screen.getByText("Default")).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText("my-secret-key"), { target: { value: "local-key" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    const createdDialog = await screen.findByRole("dialog", { name: "API key created" });
    expect(screen.getByText("sk-ant-api03-localraw")).toBeTruthy();
    const secretCard = createdDialog.querySelector('[data-slot="card"]') as HTMLElement | null;
    expect(secretCard).toBeTruthy();
    expect(secretCard?.className).toContain("bg-card");
    expect(secretCard?.className).not.toContain("bg-secondary");
    expect(api.requests.some((request) => request.method === "POST" && request.body?.name === "local-key")).toBe(true);
    expect(api.requests.filter((request) => request.method === "GET").length).toBeGreaterThan(1);
  });

  test("falls back when clipboard access is denied while copying the raw key", async () => {
    resetTestDom("https://oma.duck.ai/settings/workspaces/default/keys");
    mockWorkspaceApiKeys([activeKey]);
    const clipboardWrite = mock(async (_value: string) => {
      throw new Error("clipboard denied");
    });
    const fallbackCopy = mock((_command: string) => true);
    Object.defineProperty(globalThis.navigator, "clipboard", {
      configurable: true,
      value: { writeText: clipboardWrite },
    });
    document.execCommand = fallbackCopy as typeof document.execCommand;

    render(
      <WorkspaceApiKeysHarness>
        <WorkspaceApiKeysContent routeWorkspaceId="default" />
      </WorkspaceApiKeysHarness>,
    );

    await screen.findByText("foo");
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    fireEvent.change(screen.getByPlaceholderText("my-secret-key"), { target: { value: "local-key" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    await screen.findByRole("dialog", { name: "API key created" });
    fireEvent.click(screen.getByRole("button", { name: "Copy" }));

    await screen.findByRole("button", { name: "Copied" });
    expect(clipboardWrite).toHaveBeenCalledWith("sk-ant-api03-localraw");
    await waitFor(() => expect(fallbackCopy).toHaveBeenCalledWith("copy"));
  });

  test("falls back when clipboard access does not settle while copying the raw key", async () => {
    resetTestDom("https://oma.duck.ai/settings/workspaces/default/keys");
    mockWorkspaceApiKeys([activeKey]);
    const clipboardWrite = mock((_value: string) => new Promise<void>(() => undefined));
    const fallbackCopy = mock((_command: string) => true);
    Object.defineProperty(globalThis.navigator, "clipboard", {
      configurable: true,
      value: { writeText: clipboardWrite },
    });
    document.execCommand = fallbackCopy as typeof document.execCommand;

    render(
      <WorkspaceApiKeysHarness>
        <WorkspaceApiKeysContent routeWorkspaceId="default" />
      </WorkspaceApiKeysHarness>,
    );

    await screen.findByText("foo");
    fireEvent.click(screen.getByRole("button", { name: "Create key" }));
    fireEvent.change(screen.getByPlaceholderText("my-secret-key"), { target: { value: "local-key" } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));

    await screen.findByRole("dialog", { name: "API key created" });
    fireEvent.click(screen.getByRole("button", { name: "Copy" }));

    await screen.findByRole("button", { name: "Copied" });
    expect(clipboardWrite).toHaveBeenCalledWith("sk-ant-api03-localraw");
    await waitFor(() => expect(fallbackCopy).toHaveBeenCalledWith("copy"));
  });

  test("opens row menus, closes on outside click, and switches active versus inactive actions", async () => {
    resetTestDom("https://oma.duck.ai/settings/workspaces/default/keys");
    mockWorkspaceApiKeys([activeKey, inactiveKey]);

    render(
      <WorkspaceApiKeysHarness>
        <WorkspaceApiKeysContent routeWorkspaceId="default" />
      </WorkspaceApiKeysHarness>,
    );

    await screen.findByText("foo");
    fireEvent.click(screen.getByRole("button", { name: /More actions for foo/i }));
    expect(screen.getByRole("menu")).toBeTruthy();
    expect(screen.getByText("Disable API key")).toBeTruthy();
    expect(screen.getByText("Delete API key")).toBeTruthy();

    fireEvent.pointerDown(document.body);
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());

    fireEvent.click(screen.getByRole("button", { name: /More actions for old/i }));
    expect(screen.getByText("Enable API key")).toBeTruthy();
    expect(screen.getByText("Delete API key")).toBeTruthy();
  });

  test("disables, enables, and deletes API keys through alert dialogs", async () => {
    resetTestDom("https://oma.duck.ai/settings/workspaces/default/keys");
    const api = mockWorkspaceApiKeys([activeKey, inactiveKey]);

    render(
      <WorkspaceApiKeysHarness>
        <WorkspaceApiKeysContent routeWorkspaceId="default" />
      </WorkspaceApiKeysHarness>,
    );

    await screen.findByText("foo");

    fireEvent.click(screen.getByRole("button", { name: /More actions for foo/i }));
    fireEvent.click(screen.getByText("Disable API key"));
    expect(screen.getByRole("alertdialog", { name: "Disable key?" })).toBeTruthy();
    expect(screen.getByText("Are you sure you want to disable foo?")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Close" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Disable" }));

    await waitFor(() => expect(api.lastStatusFor("key_foo")).toBe("inactive"));
    expect(screen.getAllByText("Inactive").length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: /More actions for old/i }));
    fireEvent.click(screen.getByText("Enable API key"));
    expect(screen.getByRole("alertdialog", { name: "Enable key?" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Enable" }));

    await waitFor(() => expect(api.lastStatusFor("key_old")).toBe("active"));

    fireEvent.click(screen.getByRole("button", { name: /More actions for foo/i }));
    fireEvent.click(screen.getByText("Delete API key"));
    expect(screen.getByRole("alertdialog", { name: "Delete API key" })).toBeTruthy();
    expect(screen.getByText("Are you sure you want to delete foo? This action can't be undone.")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(api.lastStatusFor("key_foo")).toBe("archived"));
    await waitFor(() => expect(screen.queryByText("foo")).toBeNull());
  });
});

function restoreClipboard() {
  if (originalClipboardDescriptor) {
    Object.defineProperty(globalThis.navigator, "clipboard", originalClipboardDescriptor);
    return;
  }
  delete (globalThis.navigator as unknown as Record<string, unknown>).clipboard;
}

function restoreExecCommand() {
  if (originalExecCommand) {
    document.execCommand = originalExecCommand;
    return;
  }
  delete (document as unknown as Record<string, unknown>).execCommand;
}

function WorkspaceApiKeysHarness({ children }: { children: ReactNode }) {
  const queryClient = useMemo(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }), []);
  const authValue = useMemo<AuthContextValue>(
    () => ({
      account: { uuid: "acct_test", email_address: "test@example.com", display_name: "test" },
      status: "authenticated",
      refresh: async () => ({ account: { uuid: "acct_test", email_address: "test@example.com" } }),
      logout: async () => undefined,
    }),
    [],
  );
  const workspaceValue = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid: "org_test",
      workspaces: [defaultWorkspace],
      activeWorkspace: defaultWorkspace,
      activeWorkspaceId: defaultWorkspace.id,
      isLoading: false,
      error: null,
      selectWorkspace: () => undefined,
      createWorkspace: async () => defaultWorkspace,
      refreshWorkspaces: async () => undefined,
    }),
    [],
  );

  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue}>
        <WorkspaceContext.Provider value={workspaceValue}>{children}</WorkspaceContext.Provider>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

type RecordedRequest = {
  url: string;
  method: string;
  body?: Record<string, unknown>;
};

function mockWorkspaceApiKeys(initialKeys: WorkspaceApiKey[]) {
  let keys = [...initialKeys];
  const requests: RecordedRequest[] = [];

  const fetchMock = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? "GET";
    const body = parseBody(init?.body);
    requests.push({ url, method, body });

    if (url.endsWith("/api_keys") && method === "GET") {
      return jsonResponse(keys);
    }

    if (url.endsWith("/api_keys") && method === "POST") {
      const name = typeof body?.name === "string" ? body.name : "Untitled key";
      const created: WorkspaceApiKey = {
        id: "key_created",
        type: "api_key",
        name,
        raw_key: "sk-ant-api03-localraw",
        partial_key_hint: "sk-ant...lraw",
        key_prefix: "sk-ant-api03",
        key_suffix: "lraw",
        created_at: "2026-06-18T00:00:00Z",
        last_used_at: null,
        status: "active",
      };
      keys = [created, ...keys];
      return jsonResponse(created);
    }

    const keyId = url.match(/\/api_keys\/([^/?]+)$/)?.[1];
    if (keyId && method === "POST") {
      const status = body?.status;
      keys = keys.map((apiKey) =>
        apiKey.id === keyId
          ? {
              ...apiKey,
              status: typeof status === "string" ? status : apiKey.status,
              archived_at: status === "archived" ? "2026-06-18T01:00:00Z" : null,
            }
          : apiKey,
      );
      return jsonResponse(keys.find((apiKey) => apiKey.id === keyId) ?? { ...activeKey, id: keyId });
    }

    return jsonResponse({ error: { message: "not found" } }, 404);
  });

  globalThis.fetch = fetchMock as unknown as typeof fetch;

  return {
    requests,
    lastStatusFor: (apiKeyId: string) => {
      const matching = requests
        .filter((request) => request.url.endsWith(`/api_keys/${apiKeyId}`) && request.method === "POST")
        .at(-1);
      return matching?.body?.status;
    },
  };
}

function parseBody(body: BodyInit | null | undefined) {
  if (!body || typeof body !== "string") {
    return undefined;
  }
  return JSON.parse(body) as Record<string, unknown>;
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const activeKey: WorkspaceApiKey = {
  id: "key_foo",
  type: "api_key",
  name: "foo",
  partial_key_hint: "sk-ant...1234",
  key_prefix: "sk-ant-api03",
  key_suffix: "1234",
  created_at: "2026-06-18T00:00:00Z",
  last_used_at: null,
  status: "active",
};

const inactiveKey: WorkspaceApiKey = {
  id: "key_old",
  type: "api_key",
  name: "old",
  partial_key_hint: "sk-ant...5678",
  key_prefix: "sk-ant-api03",
  key_suffix: "5678",
  created_at: "2026-06-17T00:00:00Z",
  last_used_at: null,
  status: "inactive",
};

const archivedKey: WorkspaceApiKey = {
  id: "key_archived",
  type: "api_key",
  name: "archived",
  partial_key_hint: "sk-ant...9999",
  created_at: "2026-06-16T00:00:00Z",
  last_used_at: null,
  archived_at: "2026-06-18T00:00:00Z",
  status: "archived",
};
