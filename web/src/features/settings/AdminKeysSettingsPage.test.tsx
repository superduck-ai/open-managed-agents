import { afterEach, describe, expect, test } from "bun:test";
import { I18nProvider } from "../../shared/i18n";
import { resetTestDom } from "../../test/setup";
import { AdminKeysSettingsPage } from "./AdminKeysSettingsPage";

const testingLibrary = await import("@testing-library/react");
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe("Admin keys settings page", () => {
  test("creates, reveals, rotates, revokes, and deletes local preview admin keys", async () => {
    resetTestDom("https://oma.duck.ai/settings/admin-keys");

    const { container } = render(
      <I18nProvider initialLocale="en">
        <AdminKeysSettingsPage />
      </I18nProvider>,
    );

    expect(screen.getByRole("heading", { name: "Admin keys" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Create admin key" })).toBeTruthy();

    press(screen.getByRole("button", { name: "Create admin key" }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Create admin key" })).toBeTruthy());

    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Build pipeline" } });
    fireEvent.change(screen.getByLabelText("Description"), {
      target: { value: "Coordinates organization-level release automation." },
    });
    press(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(screen.getByRole("heading", { name: "Admin key value" })).toBeTruthy());
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
    expect(screen.queryByRole("heading", { name: "Create admin key" })).toBeNull();
    expect((screen.getByLabelText("Admin key") as HTMLInputElement).value).toBe("sk-ant-admin-local-0001-01");
    press(screen.getAllByRole("button", { name: "Close" })[0]);

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByText("Build pipeline")).toBeTruthy();
    expect(container.querySelectorAll('[data-slot="card"]').length).toBeGreaterThan(1);

    openRowMenu("Build pipeline");
    press(screen.getByRole("menuitem", { name: "Reveal key" }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Admin key value" })).toBeTruthy());
    expect((screen.getByLabelText("Admin key") as HTMLInputElement).value).toBe("sk-ant-admin-local-0001-01");
    press(screen.getAllByRole("button", { name: "Close" })[0]);

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    openRowMenu("Build pipeline");
    press(screen.getByRole("menuitem", { name: "Rotate key" }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Rotate admin key?" })).toBeTruthy());
    press(screen.getByRole("button", { name: "Rotate key" }));

    await waitFor(() => expect(screen.getByRole("heading", { name: "Admin key value" })).toBeTruthy());
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect((screen.getByLabelText("Admin key") as HTMLInputElement).value).toBe("sk-ant-admin-local-0001-02");
    press(screen.getAllByRole("button", { name: "Close" })[0]);

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    openRowMenu("Build pipeline");
    press(screen.getByRole("menuitem", { name: "Revoke key" }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Revoke admin key?" })).toBeTruthy());
    press(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());

    openRowMenu("Build pipeline");
    press(screen.getByRole("menuitem", { name: "Revoke key" }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Revoke admin key?" })).toBeTruthy());
    press(screen.getByRole("button", { name: "Revoke key" }));

    await waitFor(() => expect(screen.getByText("No admin keys yet")).toBeTruthy());

    press(screen.getByRole("tab", { name: "Revoked" }));
    expect(screen.getByText("Build pipeline")).toBeTruthy();

    openRowMenu("Build pipeline");
    press(screen.getByRole("menuitem", { name: "Delete preview row" }));
    await waitFor(() => expect(screen.getByRole("heading", { name: "Delete preview row?" })).toBeTruthy());
    press(screen.getByRole("button", { name: "Delete preview row" }));

    await waitFor(() => expect(screen.getByText("No revoked admin keys")).toBeTruthy());
  });
});

function openRowMenu(name: string) {
  press(screen.getByRole("button", { name: `More actions for ${name}` }));
}

function press(target: Element) {
  fireEvent.pointerDown(target);
  fireEvent.mouseDown(target);
  fireEvent.pointerUp(target);
  fireEvent.mouseUp(target);
  fireEvent.click(target);
}
