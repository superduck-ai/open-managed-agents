import { afterEach, describe, expect, test } from "bun:test";
import { I18nProvider } from "../../shared/i18n";
import { resetTestDom } from "../../test/setup";
import { WorkloadIdentitySettingsPage } from "./WorkloadIdentitySettingsPage";

const testingLibrary = await import("@testing-library/react");
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe("Workload identity settings page", () => {
  test("creates, reviews, disables, restores, and deletes local preview providers", async () => {
    resetTestDom("https://oma.duck.ai/settings/workload-identity");

    render(
      <I18nProvider initialLocale="en">
        <WorkloadIdentitySettingsPage />
      </I18nProvider>,
    );

    expect(
      screen.getByRole("heading", { name: "Workload identity" }),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "View service accounts" })
        .getAttribute("href"),
    ).toBe("/settings/service-accounts");
    expect(
      screen.getByRole("button", { name: "Create provider" }),
    ).toBeTruthy();

    press(screen.getByRole("button", { name: "Create provider" }));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Create provider" }),
      ).toBeTruthy(),
    );

    press(screen.getByRole("combobox", { name: "Provider" }));
    await waitFor(() =>
      expect(
        screen.getByRole("option", { name: "GitHub Actions" }),
      ).toBeTruthy(),
    );
    press(screen.getByRole("option", { name: "GitHub Actions" }));
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Production deploy federation" },
    });
    fireEvent.change(screen.getByLabelText("Trusted subject"), {
      target: { value: "repo:open-managed-agent/release:ref:refs/heads/main" },
    });
    press(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByText("Production deploy federation")).toBeTruthy();
    expect(screen.getByText("GitHub Actions")).toBeTruthy();
    const issuerTrigger = screen
      .getByText("https://token.actions.githubusercontent.com")
      .closest('[data-slot="tooltip-trigger"]') as HTMLElement | null;
    expect(issuerTrigger?.dataset.slot).toBe("tooltip-trigger");
    fireEvent.mouseEnter(issuerTrigger as HTMLElement);
    await waitFor(() =>
      expect(
        document.querySelector('[data-slot="tooltip-content"]')?.textContent,
      ).toBe("https://token.actions.githubusercontent.com"),
    );
    fireEvent.mouseLeave(issuerTrigger as HTMLElement);

    openRowMenu("Production deploy federation");
    press(screen.getByRole("menuitem", { name: "View trust policy" }));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Trust policy" }),
      ).toBeTruthy(),
    );
    expect(
      (screen.getByDisplayValue("GitHub Actions") as HTMLInputElement).value,
    ).toBe("GitHub Actions");
    expect(
      screen.getByDisplayValue("https://token.actions.githubusercontent.com"),
    ).toBeTruthy();
    expect(
      screen.getByDisplayValue(
        "repo:open-managed-agent/release:ref:refs/heads/main",
      ),
    ).toBeTruthy();
    press(screen.getAllByRole("button", { name: "Close" })[0]);

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    openRowMenu("Production deploy federation");
    press(screen.getByRole("menuitem", { name: "Disable provider" }));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Disable provider?" }),
      ).toBeTruthy(),
    );
    press(screen.getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());

    openRowMenu("Production deploy federation");
    press(screen.getByRole("menuitem", { name: "Disable provider" }));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Disable provider?" }),
      ).toBeTruthy(),
    );
    press(screen.getByRole("button", { name: "Disable provider" }));

    await waitFor(() =>
      expect(screen.getByText("No providers yet")).toBeTruthy(),
    );

    press(screen.getByRole("tab", { name: "Disabled" }));
    expect(screen.getByText("Production deploy federation")).toBeTruthy();

    openRowMenu("Production deploy federation");
    press(screen.getByRole("menuitem", { name: "Restore provider" }));
    await waitFor(() =>
      expect(screen.getByText("No disabled providers")).toBeTruthy(),
    );

    press(screen.getByRole("button", { name: "Show active providers" }));
    expect(screen.getByText("Production deploy federation")).toBeTruthy();

    openRowMenu("Production deploy federation");
    press(screen.getByRole("menuitem", { name: "Disable provider" }));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Disable provider?" }),
      ).toBeTruthy(),
    );
    press(screen.getByRole("button", { name: "Disable provider" }));

    await waitFor(() =>
      expect(screen.getByText("No providers yet")).toBeTruthy(),
    );

    press(screen.getByRole("tab", { name: "Disabled" }));
    openRowMenu("Production deploy federation");
    press(screen.getByRole("menuitem", { name: "Delete preview row" }));
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Delete preview row?" }),
      ).toBeTruthy(),
    );
    press(screen.getByRole("button", { name: "Delete preview row" }));

    await waitFor(() =>
      expect(screen.getByText("No disabled providers")).toBeTruthy(),
    );
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
