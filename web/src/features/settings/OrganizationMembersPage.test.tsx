import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, test } from "bun:test";
import { useMemo, type ReactNode } from "react";
import { SettingsShell } from "../../app/layout/ConsoleLayout";
import { AuthContext, type AuthContextValue } from "../../shared/auth/context";
import { defaultWorkspace } from "../../shared/workspaces/api";
import { WorkspaceContext, type WorkspaceContextValue } from "../../shared/workspaces/context";
import { resetTestDom } from "../../test/setup";
import { OrganizationMembersPage } from "./OrganizationMembersPage";
import type { OrganizationInvite, OrganizationMember } from "./membersApi";

const testingLibrary = await import("@testing-library/react");
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

describe("Organization members settings", () => {
  test("renders the official members shell and table from the console API", async () => {
    resetTestDom("https://oma.duck.ai/settings/members");
    mockMembersApi();

    render(
      <OrganizationMembersHarness>
        <SettingsShell
          currentPath="/settings/members"
          account={{
            uuid: "acct_test",
            email_address: "test@example.com",
            display_name: "test",
            memberships: [{ organization: { uuid: "org_test", name: "default" }, role: "admin" }],
          }}
          onLogout={() => undefined}
        >
          <OrganizationMembersPage />
        </SettingsShell>
      </OrganizationMembersHarness>,
    );

    expect(
      screen
        .getAllByRole("button", { name: /Default/i })
        .some((button) => button.getAttribute("aria-label") === "Default"),
    ).toBe(true);
    expect(screen.getByText("Organization settings")).toBeTruthy();
    expect(await screen.findByRole("heading", { name: "Members 3" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Invite" })).toBeTruthy();

    const table = screen.getByRole("table", { name: "Members" });
    expect(within(table).getByRole("columnheader", { name: "Name" })).toBeTruthy();
    expect(within(table).getByRole("columnheader", { name: "Email" })).toBeTruthy();
    expect(within(table).getByRole("columnheader", { name: "Role" })).toBeTruthy();
    expect(within(table).getByText("Current User")).toBeTruthy();
    expect(within(table).getByText("test@example.com")).toBeTruthy();
    expect(within(table).getByText("Pending")).toBeTruthy();
    expect(within(table).getByText("pending@example.com")).toBeTruthy();
    expect(within(table).getByRole("button", { name: "More actions" })).toBeTruthy();
    expect(within(table).getByText("Ada Lovelace")).toBeTruthy();
    expect(within(table).getByText("ada@example.com")).toBeTruthy();
    expect(screen.queryByRole("combobox", { name: "Role for Current User" })).toBeNull();
    expect(screen.getByRole("combobox", { name: "Role for Ada Lovelace" })).toBeTruthy();
  });

  test("retries both members and pending invites after a shared table load failure", async () => {
    resetTestDom("https://oma.duck.ai/settings/members");
    const api = mockMembersApi({ failMembersOnce: true, failInvitesOnce: true });

    render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );

    const alert = await screen.findByRole("alert");
    expect(within(alert).getByText("Members could not be loaded.")).toBeTruthy();
    expect(within(alert).getByText("Try again.")).toBeTruthy();
    fireEvent.click(within(alert).getByRole("button", { name: "Try again" }));

    expect(await screen.findByRole("heading", { name: "Members 3" })).toBeTruthy();
    expect(screen.getByText("pending@example.com")).toBeTruthy();
    await waitFor(() => expect(api.memberListRequests).toBe(2));
    await waitFor(() => expect(api.inviteListRequests).toBe(2));
  });

  test("mounts a shared toaster for invite actions instead of inline status chrome", async () => {
    resetTestDom("https://oma.duck.ai/settings/members");
    mockMembersApi();

    const { container } = render(
      <OrganizationMembersHarness>
        <OrganizationMembersPage />
      </OrganizationMembersHarness>,
    );

    expect(await screen.findByRole("heading", { name: "Members 3" })).toBeTruthy();
    expect(screen.getByLabelText("Notifications alt+T")).toBeTruthy();
    expect(container.querySelector('[role="status"]')).toBeNull();
    expect(container.querySelector(".text-emerald-600")).toBeNull();
  });

  test("renders the no-organization empty state without legacy surface-card chrome", () => {
    resetTestDom("https://oma.duck.ai/settings/members");

    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const authValue: AuthContextValue = {
      account: {
        uuid: "acct_test",
        email_address: "test@example.com",
        display_name: "test",
        memberships: [],
      },
      status: "authenticated",
      csrfToken: "csrf_test",
      refresh: async () => ({ account: { uuid: "acct_test", email_address: "test@example.com" } }),
      logout: async () => undefined,
    };
    const workspaceValue: WorkspaceContextValue = {
      orgUuid: null,
      workspaces: [defaultWorkspace],
      activeWorkspace: defaultWorkspace,
      activeWorkspaceId: defaultWorkspace.id,
      isLoading: false,
      error: null,
      selectWorkspace: () => undefined,
      createWorkspace: async () => defaultWorkspace,
      refreshWorkspaces: async () => undefined,
    };

    const { container } = render(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue}>
          <WorkspaceContext.Provider value={workspaceValue}>
            <SettingsShell currentPath="/settings/members" account={authValue.account} onLogout={() => undefined}>
              <OrganizationMembersPage />
            </SettingsShell>
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </QueryClientProvider>,
    );

    expect(screen.getByRole("heading", { name: "Members" })).toBeTruthy();
    expect(screen.getByText("No organization is available for this session.")).toBeTruthy();
    expect(container.querySelector(".surface-card")).toBeNull();
  });
});

function OrganizationMembersHarness({ children }: { children: ReactNode }) {
  const queryClient = useMemo(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }), []);
  const authValue = useMemo<AuthContextValue>(
    () => ({
      account: {
        uuid: "acct_test",
        email_address: "test@example.com",
        display_name: "test",
        memberships: [{ organization: { uuid: "org_test", name: "default" }, role: "admin" }],
      },
      status: "authenticated",
      csrfToken: "csrf_test",
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

function mockMembersApi(options: { failMembersOnce?: boolean; failInvitesOnce?: boolean } = {}) {
  let members: OrganizationMember[] = [
    {
      id: "acct_test",
      type: "user",
      name: "Current User",
      email: "test@example.com",
      role: "admin",
      added_at: "2026-01-01T00:00:00Z",
    },
    {
      id: "user_ada",
      type: "user",
      name: "Ada Lovelace",
      email: "ada@example.com",
      role: "user",
      added_at: "2026-01-02T00:00:00Z",
    },
  ];
  const roleUpdates: Array<{ body: Record<string, unknown>; csrfToken: string | null }> = [];
  const inviteCreates: Array<{ body: Record<string, unknown>; csrfToken: string | null }> = [];
  const inviteResends: Array<{ inviteId: string; csrfToken: string | null }> = [];
  const inviteDeletes: Array<{ inviteId: string; csrfToken: string | null }> = [];
  let memberListRequests = 0;
  let inviteListRequests = 0;
  let remainingMemberFailures = options.failMembersOnce ? 1 : 0;
  let remainingInviteFailures = options.failInvitesOnce ? 1 : 0;
  const invites: OrganizationInvite[] = [
    {
      id: "invite_pending",
      type: "invite",
      email: "pending@example.com",
      role: "billing",
      status: "pending",
      invited_at: "2026-06-24T00:00:00Z",
      expires_at: "2026-07-15T00:00:00Z",
    },
  ];

  const fetchMock = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const requestUrl = new URL(url, "https://oma.duck.ai");
    const method = init?.method ?? "GET";

    if (requestUrl.pathname === "/api/console/organizations/org_test/members" && method === "GET") {
      memberListRequests += 1;
      if (remainingMemberFailures > 0) {
        remainingMemberFailures -= 1;
        return jsonResponse({ error: "members unavailable" }, 500);
      }
      return jsonResponse(members);
    }

    if (requestUrl.pathname === "/api/console/organizations/org_test/members/user_ada" && method === "POST") {
      const body = parseBody(init?.body);
      roleUpdates.push({ body, csrfToken: headerValue(init?.headers, "X-CSRF-Token") });
      members = members.map((member) => (member.id === "user_ada" ? { ...member, role: String(body.role) } : member));
      return jsonResponse(members.find((member) => member.id === "user_ada"));
    }

    if (requestUrl.pathname === "/api/console/organizations/org_test/invites" && method === "GET") {
      inviteListRequests += 1;
      if (remainingInviteFailures > 0) {
        remainingInviteFailures -= 1;
        return jsonResponse({ error: "invites unavailable" }, 500);
      }
      const status = requestUrl.searchParams.get("status");
      return jsonResponse(status ? invites.filter((invite) => invite.status === status) : invites);
    }

    if (requestUrl.pathname === "/api/console/organizations/org_test/invites" && method === "POST") {
      const body = parseBody(init?.body);
      inviteCreates.push({ body, csrfToken: headerValue(init?.headers, "X-CSRF-Token") });
      const invite: OrganizationInvite = {
        id: `invite_${inviteCreates.length}`,
        type: "invite",
        email: String(body.email).toLowerCase(),
        role: String(body.role),
        status: "pending",
        invited_at: "2026-06-25T00:00:00Z",
        expires_at: "2026-07-16T00:00:00Z",
      };
      invites.push(invite);
      return jsonResponse(invite);
    }

    const inviteActionPrefix = "/api/console/organizations/org_test/invites/";
    if (requestUrl.pathname.startsWith(inviteActionPrefix)) {
      const inviteId = decodeURIComponent(requestUrl.pathname.slice(inviteActionPrefix.length));
      const inviteIndex = invites.findIndex((invite) => invite.id === inviteId);

      if (method === "PUT") {
        inviteResends.push({ inviteId, csrfToken: headerValue(init?.headers, "X-CSRF-Token") });
        if (inviteIndex < 0) {
          return jsonResponse({ error: "invite not found" }, 404);
        }
        invites[inviteIndex] = {
          ...invites[inviteIndex],
          status: "pending",
          invited_at: "2026-06-26T00:00:00Z",
          expires_at: "2026-07-17T00:00:00Z",
        };
        return jsonResponse(invites[inviteIndex]);
      }

      if (method === "DELETE") {
        inviteDeletes.push({ inviteId, csrfToken: headerValue(init?.headers, "X-CSRF-Token") });
        if (inviteIndex < 0) {
          return jsonResponse({ error: "invite not found" }, 404);
        }
        invites[inviteIndex] = { ...invites[inviteIndex], status: "deleted" };
        return jsonResponse({ id: inviteId, type: "invite_deleted" });
      }
    }

    return jsonResponse({ error: "not found" }, 404);
  };

  globalThis.fetch = fetchMock as unknown as typeof fetch;
  return {
    inviteCreates,
    inviteDeletes,
    inviteResends,
    roleUpdates,
    get memberListRequests() {
      return memberListRequests;
    },
    get inviteListRequests() {
      return inviteListRequests;
    },
  };
}

function parseBody(body: BodyInit | null | undefined) {
  if (typeof body !== "string" || body === "") {
    return {} as Record<string, unknown>;
  }
  return JSON.parse(body) as Record<string, unknown>;
}

function headerValue(headers: HeadersInit | undefined, name: string) {
  if (!headers) {
    return null;
  }
  return new Headers(headers).get(name);
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type": "application/json",
    },
  });
}
