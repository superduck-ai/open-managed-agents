import { afterEach, describe, expect, mock, test } from "bun:test";
import { resetTestDom } from "../../test/setup";

const testingLibrary = await import("@testing-library/react");
const { WorkbenchPage } = await import("./WorkbenchPage");
const { WorkspaceContext } = await import("../../shared/workspaces/context");
const { AuthContext } = await import("../../shared/auth/context");
const { defaultWorkspace } = await import("../../shared/workspaces/api");
const { setConsoleRequestContext } = await import("../../shared/api/client");

const { act, cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;
const originalFetch = globalThis.fetch;

function selectOption(name: string) {
  const option = screen.getByRole("option", { name });
  fireEvent.pointerDown(option);
  fireEvent.mouseDown(option);
  fireEvent.pointerUp(option);
  fireEvent.mouseUp(option);
  fireEvent.click(option);
}

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

describe("WorkbenchPage", () => {
  test("loads the Workbench editor with official default controls", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    expect(await screen.findByRole("button", { name: "Get Code" })).toBeTruthy();
    expect(window.location.pathname).toBe("/workbench/prompt_1");
    expect(screen.getByRole("button", { name: "Run ⌘ + ⏎" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Model settings" })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Variables/i })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Tools/i })).toBeTruthy();
    expect(screen.getByLabelText("User prompt 1")).toBeTruthy();
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toContain(
      "Draft an email responding to a customer complaint email",
    );
    expect(screen.getByRole("heading", { name: "Response" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Create New Prompt" })).toBeNull();
    expect(document.body.textContent).not.toContain("Show Ideal Outputs");
  });

  test("routes bare Workbench to the most recent prompt created by the current user in the current workspace", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({
      promptSummaries: [
        {
          id: "prompt_other_workspace",
          name: "Other workspace",
          workspace_id: "wrkspc_other",
          updated_at: "2026-06-22T09:00:00.000000Z",
          creator: defaultPromptCreator(),
        },
        {
          id: "prompt_other_user",
          name: "Other user",
          workspace_id: "default",
          updated_at: "2026-06-23T09:00:00.000000Z",
          creator: { tagged_id: "user_other" },
        },
        {
          id: "prompt_recent_own",
          name: "Recent own prompt",
          workspace_id: "default",
          updated_at: "2026-06-21T09:00:00.000000Z",
          creator: defaultPromptCreator(),
        },
        {
          id: "prompt_old_own",
          name: "Old own prompt",
          workspace_id: "default",
          updated_at: "2026-06-10T09:00:00.000000Z",
          creator: defaultPromptCreator(),
        },
      ],
      promptTexts: {
        prompt_recent_own: "Most recent prompt for this user and workspace.",
      },
    });
    renderWorkbench();

    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_recent_own"));
    expect(await screen.findByRole("button", { name: "Get Code" })).toBeTruthy();
    expect(api.requests.some((request) => request.url.endsWith("/workbench/prompts") && request.method === "GET")).toBe(
      true,
    );
    expect(
      api.requests.some((request) => request.url.endsWith("/workspaces/default/prompts") && request.method === "GET"),
    ).toBe(false);
  });

  test("routes bare Workbench to new when no prompt matches the current user and workspace", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({
      promptSummaries: [
        {
          id: "prompt_other_user",
          name: "Other user",
          workspace_id: "default",
          updated_at: "2026-06-23T09:00:00.000000Z",
          creator: { tagged_id: "user_other" },
        },
        {
          id: "prompt_other_workspace",
          name: "Other workspace",
          workspace_id: "wrkspc_other",
          updated_at: "2026-06-22T09:00:00.000000Z",
          creator: defaultPromptCreator(),
        },
      ],
    });
    renderWorkbench();

    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    const listIndex = api.requests.findIndex(
      (request) => request.url.endsWith("/workbench/prompts") && request.method === "GET",
    );
    const createIndex = api.requests.findIndex(
      (request) => request.url.endsWith("/workspaces/default/prompts") && request.method === "POST",
    );
    expect(listIndex).toBeGreaterThanOrEqual(0);
    expect(createIndex).toBeGreaterThan(listIndex);
  });

  test("shows Workbench access unavailable before fetching prompts", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench({
      auth: authContextValue({
        ...defaultAuthAccount(),
        memberships: [
          {
            role: "developer",
            seat_tier: "enterprise_standard",
            organization: {
              uuid: "org_test",
              name: "Claude Platform",
              settings: {
                product_name: "Claude Platform",
                workbench: { enabled: false },
              } as any,
            },
          },
        ],
      }),
    });

    expect(await screen.findByText("Workbench access unavailable")).toBeTruthy();
    expect(screen.getByText("Claude Platform doesn't include access to the Workbench.")).toBeTruthy();
    expect(screen.getByText("Your organization has disabled Workbench access for your user role.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Go to Dashboard" })).toBeTruthy();
    expect(api.requests).toHaveLength(0);
  });

  test("renders the standardized load failure alert and retries successfully", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1");
    const api = mockWorkbenchApi({ failWorkspacePromptsOnce: true });
    renderWorkbench();

    const alert = await screen.findByRole("alert");
    expect(alert.getAttribute("data-slot")).toBe("alert");
    expect(alert.textContent).toContain("Workbench could not load");
    expect(alert.textContent).toContain("Prompt list unavailable.");

    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));

    await waitFor(() => expect(screen.getByRole("button", { name: "Get Code" })).toBeTruthy());
    expect(
      api.requests.filter((request) => request.method === "GET" && request.url.endsWith("/workspaces/default/prompts")),
    ).toHaveLength(2);
  });

  test("uses the first prompt message as the visible title for unnamed non-empty prompts", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1");
    mockWorkbenchApi({
      initialPromptName: "",
      initialPromptText: "Write a haiku about {{ANIMAL}}.",
    });
    renderWorkbench();

    const titleButton = await screen.findByRole("button", { name: "Prompt settings" });
    expect(titleButton.textContent).toContain("Write a haiku about {{ANIMAL}}");
    expect(titleButton.textContent).not.toContain("Untitled");
    expect(titleButton.textContent).not.toContain("{{ANIMAL}}.");
  });

  test("opens System Prompt as the compact official editor block", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    const collapsedSystemPrompt = screen.getByLabelText("Click to open system prompt");
    expect(collapsedSystemPrompt.tagName).toBe("DIV");
    expect(within(collapsedSystemPrompt).getAllByRole("button")).toHaveLength(2);
    fireEvent.click(collapsedSystemPrompt);

    const systemPrompt = screen.getByRole("textbox", { name: "System prompt" }) as HTMLElement & { value: string };
    expect(systemPrompt.tagName).toBe("DIV");
    expect(systemPrompt.getAttribute("contenteditable")).toBe("true");
    expect(systemPrompt.className).toContain("workbench-system-editor");
    expect(screen.getByText("System Prompt")).toBeTruthy();
    expect(document.querySelector(".workbench-system-card.is-open")).toBeTruthy();
    expect(screen.queryByText("Define a role, tone or context (optional)")).toBeNull();

    fireEvent.change(systemPrompt, { target: { value: "Answer like a precise API assistant." } });
    expect(systemPrompt.value).toBe("Answer like a precise API assistant.");
    fireEvent.click(screen.getByRole("button", { name: "Close system prompt" }));
    expect(screen.queryByLabelText("System prompt")).toBeNull();
    expect(screen.getByLabelText("Click to open system prompt")).toBeTruthy();
  });

  test("opens the official-style model settings panel", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Model settings" }));

    const panel = screen.getByLabelText("Model");
    expect(within(panel).getByRole("heading", { name: "Model" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Close" })).toBeTruthy();
    const modelCombobox = within(panel).getByRole("combobox", { name: "claude-opus-4-8" });
    expect(modelCombobox).toBeTruthy();
    expect(within(panel).queryByRole("option", { name: /claude-opus-4-8/i })).toBeNull();
    await act(async () => {
      fireEvent.click(modelCombobox);
    });
    expect(screen.getByRole("combobox", { name: "Search models…" })).toBeTruthy();
    expect(screen.getByRole("option", { name: /claude-opus-4-8/i })).toBeTruthy();
    expect(screen.getByText("Powerful, large model for complex challenges")).toBeTruthy();
    await act(async () => {
      fireEvent.click(screen.getByRole("option", { name: /claude-opus-4-8/i }));
    });
    expect(within(panel).getByRole("button", { name: "Maximum length of Claude’s responses" })).toBeTruthy();
    const maxTokensInput = within(panel)
      .getAllByRole("spinbutton")
      .find((input) => (input as HTMLInputElement).max === "128000") as HTMLInputElement;
    const maxTokensSlider = within(panel).getByRole("slider", { name: "Max tokens" }) as HTMLInputElement;
    expect(maxTokensInput).toBeTruthy();
    expect(maxTokensInput.max).toBe("128000");
    expect(maxTokensInput.step).toBe("1");
    expect(maxTokensSlider.max).toBe("128000");
    expect(maxTokensSlider.step).toBe("1");
    expect(within(panel).queryByRole("slider", { name: "Temperature" })).toBeNull();
    expect(within(panel).getByRole("radio", { name: "Enabled" }).getAttribute("aria-checked")).toBe("true");
    expect(within(panel).getByRole("button", { name: "Budget tokens" })).toBeTruthy();
    const budgetInput = within(panel)
      .getAllByRole("spinbutton")
      .find((input) => (input as HTMLInputElement).max === "127999") as HTMLInputElement;
    const budgetSlider = within(panel).getByRole("slider", { name: "Budget tokens" }) as HTMLInputElement;
    expect(budgetInput).toBeTruthy();
    expect(budgetInput.min).toBe("1024");
    expect(budgetInput.max).toBe("127999");
    expect(budgetInput.step).toBe("1");
    expect(budgetInput.value).toBe("16000");
    expect(budgetSlider.min).toBe("1024");
    expect(budgetSlider.max).toBe("127999");
    expect(budgetSlider.step).toBe("1");
    expect(budgetSlider.value).toBe("16000");
    fireEvent.click(within(panel).getByRole("radio", { name: "Disabled" }));
    const temperatureSlider = within(panel).getByRole("slider", { name: "Temperature" }) as HTMLInputElement;
    expect(temperatureSlider.value).toBe("1");
    expect(within(panel).queryByRole("slider", { name: "Budget tokens" })).toBeNull();
    fireEvent.click(within(panel).getByRole("radio", { name: "Enabled" }));
    expect(within(panel).queryByRole("slider", { name: "Temperature" })).toBeNull();
    expect(within(panel).getByRole("button", { name: "Budget tokens" })).toBeTruthy();
    const effortCombobox = within(panel).getByRole("combobox", { name: "Effort" });
    expect(effortCombobox).toBeTruthy();
    fireEvent.click(effortCombobox);
    const extraHighOption = await screen.findByRole("option", { name: "Extra high" });
    expect(extraHighOption).toBeTruthy();
    expect(screen.getByRole("option", { name: "Max" })).toBeTruthy();
    selectOption("Extra high");
    await waitFor(() => expect(effortCombobox.textContent).toContain("Extra high"));
    expect(within(panel).getByRole("link", { name: "View all API options" }).getAttribute("href")).toBe(
      "https://docs.claude.com/en/api/messages",
    );
    expect(within(panel).getByRole("button", { name: "Run ⌘ + ⏎" })).toBeTruthy();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByLabelText("Model")).toBeNull();
  });

  test("opens the official-style tools panel and custom tool form", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Tools" }));

    const panel = document.querySelector(".workbench-drawer-panel.is-tools") as HTMLElement;
    expect(panel).toBeTruthy();
    expect(within(panel).getByRole("heading", { name: "Tools" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Close" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Custom" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Web search" })).toBeTruthy();
    expect(within(panel).getByText("No tools defined")).toBeTruthy();
    expect(within(panel).getByRole("link", { name: "Learn more" })).toBeTruthy();

    const drawerBody = panel.querySelector(".workbench-drawer-body") as HTMLElement;
    fireEvent.click(within(panel).getByRole("button", { name: "Custom" }));
    await waitFor(() => expect(drawerBody.scrollTop).toBe(0));
    expect((within(panel).getByRole("button", { name: "Custom" }) as HTMLButtonElement).disabled).toBe(true);
    expect((within(panel).getByRole("button", { name: "Web search" }) as HTMLButtonElement).disabled).toBe(true);
    expect(within(panel).getByRole("heading", { name: "Custom Tool" })).toBeTruthy();
    const nameInput = within(panel).getByRole("textbox", { name: "Name" });
    expect(nameInput).toBeTruthy();
    expect(document.activeElement).toBe(nameInput);
    expect(within(panel).getByRole("textbox", { name: "Description" })).toBeTruthy();
    expect((within(panel).getByRole("textbox", { name: "Input Schema" }) as HTMLTextAreaElement).value).toContain(
      "The city and state, e.g. San Francisco, CA",
    );
    expect(panel.querySelector(".workbench-tool-schema code.language-json")).toBeTruthy();
    expect(panel.querySelector(".workbench-tool-schema .hljs-attr")).toBeTruthy();
    expect(within(panel).getByRole("link", { name: "JSON Schema (opens in new tab)" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Example tools" }).getAttribute("aria-expanded")).toBe("false");
    expect(within(panel).getByRole("button", { name: "Cancel" })).toBeTruthy();
    expect((within(panel).getByRole("button", { name: "Add tool" }) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => {
      fireEvent.click(within(panel).getByRole("button", { name: "Example tools" }));
    });
    await waitFor(() =>
      expect(within(panel).getByRole("button", { name: "Example tools" }).getAttribute("aria-expanded")).toBe("true"),
    );
    expect(screen.getByRole("menuitemradio", { name: "get_weather" })).toBeTruthy();
    expect(screen.getByRole("menuitemradio", { name: "get_time" })).toBeTruthy();
    expect(screen.queryByRole("menuitemradio", { name: "Calculator" })).toBeNull();
    fireEvent.click(screen.getByRole("menuitemradio", { name: "get_stock_price" }));
    expect((within(panel).getByRole("textbox", { name: "Name" }) as HTMLInputElement).value).toBe("get_stock_price");
    expect((within(panel).getByRole("button", { name: "Add tool" }) as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(within(panel).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(drawerBody.scrollTop).toBe(0));
    fireEvent.click(within(panel).getByRole("button", { name: "Web search" }));
    await waitFor(() => expect(drawerBody.scrollTop).toBe(0));
    expect(within(panel).getByRole("heading", { name: "Web search" })).toBeTruthy();
    expect(
      within(panel).getByText("Allow Claude to search the web and cite those results in its responses."),
    ).toBeTruthy();
    const restrictionSelect = within(panel).getByRole("combobox", { name: "Search restrictions" });
    expect(restrictionSelect.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(restrictionSelect);
    expect(screen.getByRole("option", { name: "Allow domains Only search allowed domains" })).toBeTruthy();
    expect(screen.getByRole("option", { name: "None Search any domain" }).getAttribute("aria-selected")).toBe("true");
    selectOption("Allow domains Only search allowed domains");
    await waitFor(() =>
      expect(within(panel).getByRole("combobox", { name: "Search restrictions" }).textContent).toContain(
        "Allow domains",
      ),
    );
    expect(within(panel).getByRole("combobox", { name: "Search restrictions" }).getAttribute("aria-expanded")).toBe(
      "false",
    );
    expect(
      within(panel).getByPlaceholderText("Enter domains (e.g., example.com, example.com/path, example.com/*)"),
    ).toBeTruthy();
    expect((within(panel).getByRole("button", { name: "Add tool" }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.change(
      within(panel).getByPlaceholderText("Enter domains (e.g., example.com, example.com/path, example.com/*)"),
      {
        target: { value: "docs.anthropic.com" },
      },
    );
    expect(within(panel).getByRole("switch", { name: "Limit the number of times this tool is called" })).toBeTruthy();
    expect(within(panel).getByRole("switch", { name: "Localize results" })).toBeTruthy();
    expect((within(panel).getByRole("button", { name: "Add tool" }) as HTMLButtonElement).disabled).toBe(false);
  });

  test("renders an existing Web search tool as the official card and edits it in place", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ initialTools: [{ type: "web_search_v0", name: "web_search" }] });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: /Tools/ }));

    let panel = document.querySelector(".workbench-drawer-panel.is-tools") as HTMLElement;
    expect(panel).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Custom" })).toBeTruthy();
    expect(within(panel).queryByRole("button", { name: "Web search" })).toBeNull();
    expect(within(panel).getByRole("heading", { name: "Web search" })).toBeTruthy();
    expect(within(panel).getByText("No search restrictions")).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Edit Web search" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Remove Web search" })).toBeTruthy();
    expect((within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }) as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(within(panel).getByRole("button", { name: "Edit Web search" }));
    expect(within(panel).getByRole("combobox", { name: "Search restrictions" }).textContent).toContain("None");
    fireEvent.click(within(panel).getByRole("combobox", { name: "Search restrictions" }));
    selectOption("Blocked domains Do not search blocked domains");
    await waitFor(() =>
      expect(within(panel).getByRole("combobox", { name: "Search restrictions" }).textContent).toContain(
        "Blocked domains",
      ),
    );
    fireEvent.change(
      within(panel).getByPlaceholderText("Enter domains (e.g., example.com, example.com/path, example.com/*)"),
      {
        target: { value: "https://example.com/docs" },
      },
    );
    fireEvent.click(within(panel).getByRole("button", { name: "Save tool" }));

    await waitFor(() => expect(document.querySelector(".workbench-drawer-panel.is-tools")).toBeNull());
    expect(screen.getByRole("button", { name: /Tools/ }).textContent).toContain("1");
    fireEvent.click(screen.getByRole("button", { name: /Tools/ }));
    panel = document.querySelector(".workbench-drawer-panel.is-tools") as HTMLElement;
    expect(within(panel).getByText("Blocked domain: example.com")).toBeTruthy();

    fireEvent.click(within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }));
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.tools).toEqual([
      { type: "web_search_v0", name: "web_search", blocked_domains: ["example.com"] },
    ]);
  });

  test("adds Web search restrictions to the Workbench completion payload", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Tools" }));
    const panel = document.querySelector(".workbench-drawer-panel.is-tools") as HTMLElement;
    expect(panel).toBeTruthy();
    fireEvent.click(within(panel).getByRole("button", { name: "Web search" }));
    fireEvent.click(within(panel).getByRole("combobox", { name: "Search restrictions" }));
    selectOption("Allow domains Only search allowed domains");
    await waitFor(() =>
      expect(within(panel).getByRole("combobox", { name: "Search restrictions" }).textContent).toContain(
        "Allow domains",
      ),
    );
    fireEvent.change(
      within(panel).getByPlaceholderText("Enter domains (e.g., example.com, example.com/path, example.com/*)"),
      {
        target: { value: "https://docs.anthropic.com, Example.com/docs" },
      },
    );
    fireEvent.click(within(panel).getByRole("switch", { name: "Limit the number of times this tool is called" }));
    fireEvent.change(within(panel).getByLabelText("Web search max uses"), { target: { value: "3" } });
    fireEvent.click(within(panel).getByRole("switch", { name: "Localize results" }));
    fireEvent.click(within(panel).getByRole("button", { name: "Add tool" }));

    await waitFor(() => expect(document.querySelector(".workbench-drawer-panel.is-tools")).toBeNull());
    expect(screen.getByRole("button", { name: "Tools" }).textContent).toContain("1");
    fireEvent.click(screen.getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.tools).toEqual([
      {
        type: "web_search_v0",
        name: "web_search",
        max_uses: 3,
        user_location: { type: "approximate" },
        allowed_domains: ["docs.anthropic.com", "example.com"],
      },
    ]);
  });

  test("uploads a file attachment from a user prompt and sends it in the completion payload", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ initialPromptText: "Summarize this." });
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    fireEvent.click(screen.getByRole("button", { name: "Upload up to 100 files, 20MB per file." }));
    expect(screen.getByRole("menuitem", { name: "Image" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "PDF" })).toBeTruthy();
    fireEvent.click(screen.getByRole("menuitem", { name: "Image" }));
    expect(screen.getByRole("menuitem", { name: "Upload from device" })).toBeTruthy();
    expect(screen.getByRole("menuitem", { name: "Add from URL" })).toBeTruthy();
    expect(screen.getByTestId("workbench-upload-input-0-image").getAttribute("accept")).toBe("image/*");
    expect(screen.getByTestId("workbench-upload-input-0").getAttribute("accept")).toBe(".pdf,application/pdf");
    fireEvent.click(document.body);

    fireEvent.change(screen.getByTestId("workbench-upload-input-0"), {
      target: { files: [new File(["hello world"], "brief.pdf", { type: "application/pdf" })] },
    });

    expect(await screen.findByText("brief.pdf")).toBeTruthy();
    const attachments = screen.getByLabelText("User prompt attachments");
    expect((within(attachments).getByRole("button", { name: "Preview brief.pdf" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    fireEvent.click(within(attachments).getByRole("button", { name: "Replace brief.pdf" }));
    fireEvent.change(screen.getByTestId("workbench-upload-input-0"), {
      target: { files: [new File(["updated brief"], "brief-v2.pdf", { type: "application/pdf" })] },
    });
    expect(await screen.findByText("brief-v2.pdf")).toBeTruthy();
    expect(screen.queryByText("brief.pdf")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Remove brief-v2.pdf" }));
    expect(screen.queryByText("brief-v2.pdf")).toBeNull();
    fireEvent.change(screen.getByTestId("workbench-upload-input-0"), {
      target: { files: [new File(["hello world"], "brief.pdf", { type: "application/pdf" })] },
    });
    expect(await screen.findByText("brief.pdf")).toBeTruthy();
    fireEvent.change(userPrompt, { target: { value: "Summarize this file." } });
    fireEvent.click(screen.getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    const uploadRequest = api.requests.find((request) => request.url === "/v1/files?beta=true");
    expect(uploadRequest?.method).toBe("POST");
    expect(uploadRequest?.headers["anthropic-beta"]).toBe("files-api-2025-04-14");
    expect(api.requests.filter((request) => request.url === "/v1/files?beta=true")).toHaveLength(3);
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.messages?.[0]?.content).toEqual([
      { type: "text", text: "Summarize this file." },
      {
        type: "document",
        source: { type: "file", file_id: "file_upload_test" },
        title: "brief.pdf",
      },
    ]);
  });

  test("adds image and PDF attachments from URLs without uploading a file", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ initialPromptText: "Describe these attachments." });
    renderWorkbench();

    await screen.findByLabelText("User prompt 1");
    fireEvent.click(screen.getByRole("button", { name: "Upload up to 100 files, 20MB per file." }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Image" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Add from URL" }));

    let dialog = screen.getByRole("dialog", { name: "Add an image via URL" });
    const imageUrlInput = within(dialog).getByRole("textbox", { name: "URL" }) as HTMLInputElement;
    expect(imageUrlInput.getAttribute("placeholder")).toBe("https://example.com/image.jpg");
    expect((within(dialog).getByRole("button", { name: "Add" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(imageUrlInput, { target: { value: "https://example.com/image.jpg" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add" }));

    expect(await screen.findByText("Image")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Upload up to 100 files, 20MB per file." }));
    fireEvent.click(screen.getByRole("menuitem", { name: "PDF" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Add from URL" }));

    dialog = screen.getByRole("dialog", { name: "Add a PDF via URL" });
    const pdfUrlInput = within(dialog).getByRole("textbox", { name: "URL" }) as HTMLInputElement;
    expect(pdfUrlInput.getAttribute("placeholder")).toBe("https://example.com/document.pdf");
    fireEvent.change(pdfUrlInput, { target: { value: "https://example.com/document.pdf" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add" }));

    expect(await screen.findByText("PDF")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    expect(api.requests.filter((request) => request.url === "/v1/files?beta=true")).toHaveLength(0);
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.messages?.[0]?.content).toEqual([
      { type: "text", text: "Describe these attachments." },
      { type: "image", source: { type: "url", url: "https://example.com/image.jpg" } },
      { type: "document", source: { type: "url", url: "https://example.com/document.pdf" } },
    ]);
  });

  test("shows the official file drop target and uploads dropped files into the user prompt", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ initialPromptText: "Summarize this." });
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    const messageCard = userPrompt.closest(".workbench-message-card") as HTMLElement;
    const droppedFile = new File(["meeting notes"], "notes.txt", { type: "text/plain" });

    fireEvent.dragEnter(messageCard, {
      dataTransfer: { types: ["Files"], files: [droppedFile] },
    });
    expect(messageCard.classList.contains("is-drag-over")).toBe(true);
    expect(screen.getByText("Drop here to insert into user message")).toBeTruthy();
    expect(screen.getByText("Max 100 files at 20MB each")).toBeTruthy();

    fireEvent.drop(messageCard, {
      dataTransfer: { types: ["Files"], files: [droppedFile] },
    });

    expect(await screen.findByText("notes.txt")).toBeTruthy();
    expect(messageCard.classList.contains("is-drag-over")).toBe(false);
    expect(api.requests.filter((request) => request.url === "/v1/files?beta=true")).toHaveLength(1);
  });

  test("opens the official-style Examples panel and add-example form", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    fireEvent.change(userPrompt, { target: { value: "Write a haiku about {{animal}}." } });
    expect((screen.getByRole("button", { name: "Pre-fill response" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Help Claude understand the task better" }));

    const panel = screen.getByLabelText("Examples");
    expect(within(panel).getByRole("heading", { name: "Examples" })).toBeTruthy();
    expect(within(panel).getByText("No examples defined")).toBeTruthy();
    expect(within(panel).getByRole("link", { name: "Learn more" })).toBeTruthy();

    fireEvent.click(within(panel).getByRole("button", { name: "Add example" }));
    expect((within(panel).getByRole("button", { name: "Add example" }) as HTMLButtonElement).disabled).toBe(true);
    expect(within(panel).getByRole("heading", { name: "Add example" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Generate example" })).toBeTruthy();
    expect(within(panel).getByRole("textbox", { name: "{{animal}}" })).toBeTruthy();
    expect(within(panel).getByPlaceholderText("Enter an example value...")).toBeTruthy();
    expect(within(panel).getByRole("textbox", { name: "Ideal output" })).toBeTruthy();
    expect(within(panel).getByPlaceholderText("Enter ideal output...")).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Add additional context" })).toBeTruthy();
    expect((within(panel).getByRole("button", { name: "Add Example" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(within(panel).getByRole("button", { name: "Generate example" }));
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/evaluations/generate_test_case"))).toBe(
        true,
      ),
    );
    const generateRequest = api.requests.find((request) =>
      request.url.endsWith("/workbench/evaluations/generate_test_case"),
    );
    expect(Object.keys(generateRequest?.body ?? {})).toEqual([
      "system_prompt",
      "messages",
      "custom_chain_of_thought",
      "existing_examples",
    ]);
    expect(generateRequest?.body?.messages.at(-1)).toEqual({
      role: "assistant",
      content: [{ type: "text", text: "" }],
    });
    expect(generateRequest?.body).not.toHaveProperty("model_name");
    expect(generateRequest?.body).not.toHaveProperty("ideal_output");
    expect(generateRequest?.body).not.toHaveProperty("additional_context");
    await waitFor(() =>
      expect((within(panel).getByRole("textbox", { name: "{{animal}}" }) as HTMLTextAreaElement).value).toBe("owl"),
    );
    expect((within(panel).getByRole("textbox", { name: "Ideal output" }) as HTMLTextAreaElement).value).toBe(
      "A quiet owl answers in moonlight.",
    );
  });

  test("loads existing Examples as official-style cards and includes them in run payload", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1");
    const api = mockWorkbenchApi({
      initialPromptName: "",
      initialPromptText: "Write a haiku about {{ANIMAL}}.",
      promptExamples: [
        {
          variable_values: { ANIMAL: "Generated ANIMAL example 1" },
          ideal_output: "A tiny owl poem.",
        },
        {
          variable_values: { ANIMAL: "falcon" },
          ideal_output: "A swift sky poem.",
          additional_context: "Keep the tone playful.",
        },
      ],
    });
    renderWorkbench();

    const examplesButton = await screen.findByRole("button", { name: "Help Claude understand the task better" });
    expect(examplesButton.textContent?.replace(/\s/g, "")).toBe("Examples2");
    fireEvent.click(examplesButton);

    const panel = screen.getByLabelText("Examples");
    expect(within(panel).queryByText("No examples defined")).toBeNull();
    expect(within(panel).getByText("ANIMAL: Generated ANIMAL example 1")).toBeTruthy();
    expect(within(panel).getByText("A tiny owl poem.")).toBeTruthy();
    expect(within(panel).getByText("ANIMAL: falcon")).toBeTruthy();
    expect(within(panel).getByText("A swift sky poem.")).toBeTruthy();
    expect(within(panel).getByText("Additional context:")).toBeTruthy();
    expect(within(panel).getByText("Keep the tone playful.")).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Edit example 1" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Delete example 1" })).toBeTruthy();

    fireEvent.click(within(panel).getByRole("button", { name: "Edit example 2" }));
    expect(within(panel).getByRole("heading", { name: "Edit example" })).toBeTruthy();
    expect((within(panel).getByRole("textbox", { name: "{{ANIMAL}}" }) as HTMLTextAreaElement).value).toBe("falcon");
    expect((within(panel).getByRole("textbox", { name: "Ideal output" }) as HTMLTextAreaElement).value).toBe(
      "A swift sky poem.",
    );
    fireEvent.click(within(panel).getByRole("button", { name: "Cancel" }));

    fireEvent.click(screen.getByRole("button", { name: /Variables/i }));
    const variablesPanel = await screen.findByLabelText("Test Case");
    fireEvent.change(within(variablesPanel).getByLabelText("{{ANIMAL}}"), { target: { value: "owl" } });
    fireEvent.click(screen.getByRole("button", { name: "Help Claude understand the task better" }));
    fireEvent.click(within(screen.getByLabelText("Examples")).getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    const firstMessage = runRequest?.body?.messages?.[0]?.content?.[0]?.text;
    expect(firstMessage).toContain("<examples>");
    expect(firstMessage).toContain("<ANIMAL>");
    expect(firstMessage).toContain("falcon");
    expect(firstMessage).toContain("<ideal_output>");
    expect(firstMessage).toContain("A swift sky poem.");
    expect(firstMessage).toContain("<example_description>");
    expect(firstMessage).toContain("Keep the tone playful.");
  });

  test("opens the official-style Improve prompt modal and sends generate_prompt feedback", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Use Claude to optimize your prompt" }));

    const dialog = screen.getByRole("dialog", { name: "What would you like to improve?" });
    expect(within(dialog).getByRole("button", { name: "Close" })).toBeTruthy();
    expect(within(dialog).getByText("This takes 1-2 minutes and uses Claude Sonnet 4.5 credits")).toBeTruthy();
    const feedback = within(dialog).getByRole("textbox", {
      name: "The more detailed the feedback, the more Claude will be able to help.",
    });
    expect(feedback.getAttribute("data-slot")).toBe("textarea");
    expect(feedback.className.includes("bg-secondary")).toBe(false);
    fireEvent.change(feedback, { target: { value: "Make it warmer and shorter." } });
    expect(
      within(dialog)
        .getByRole("checkbox", {
          name: "This prompt will be used with models that have thinking enabled",
        })
        .getAttribute("aria-checked"),
    ).toBe("true");
    fireEvent.click(within(dialog).getByRole("button", { name: "Improve prompt" }));

    await waitFor(() =>
      expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("Improved prompt."),
    );
    const request = api.requests.find((item) => item.url.endsWith("/workbench/generate_prompt"));
    expect(request?.method).toBe("POST");
    expect(request?.body?.feedback).toBe("Make it warmer and shorter.");
    expect(request?.body?.thinking_enabled).toBe(true);
  });

  test("shows Generate Prompt inside the first empty user message and sends a zero-generation request", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const generatedPrompt = "Write a crisp project brief about {{topic}}.";
    const api = mockWorkbenchApi({
      initialPromptName: "",
      initialPromptText: "",
      generatedPromptDelayMs: 25,
      generatedPromptText:
        "<planning>Draft the reusable prompt.</planning>\n<Instructions>Write a crisp project brief about {$topic}.\nLet me know if you want me to adjust this.</Instructions>",
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    expect(screen.getAllByRole("button", { name: "Generate Prompt" })).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Add message pair" }));
    expect(screen.getAllByRole("button", { name: "Generate Prompt" })).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "Generate Prompt" }));
    const dialog = screen.getByRole("dialog", { name: "Generate a prompt" });
    expect(
      within(dialog)
        .getByRole("checkbox", {
          name: "This prompt will be used with models that have thinking enabled",
        })
        .getAttribute("aria-checked"),
    ).toBe("true");
    const taskTextarea = within(dialog).getByRole("textbox", { name: "Describe your task..." });
    expect(taskTextarea.getAttribute("data-slot")).toBe("textarea");
    expect(taskTextarea.className.includes("bg-secondary")).toBe(false);
    fireEvent.change(taskTextarea, {
      target: { value: "Summarize meeting notes into action items." },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Generate" }));

    expect(within(dialog).getByRole("heading", { name: "Your prompt" })).toBeTruthy();
    expect(within(dialog).getByText("Generating prompt…")).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeTruthy();
    expect((within(dialog).getByRole("button", { name: "Continue" }) as HTMLButtonElement).disabled).toBe(true);

    const generatedPromptTextarea = (await within(dialog).findByRole("textbox", {
      name: "Your prompt",
    })) as HTMLTextAreaElement;
    await waitFor(() => expect(generatedPromptTextarea.value).toBe(generatedPrompt));
    expect(within(dialog).getByRole("heading", { name: "Variables" })).toBeTruthy();
    expect(
      within(dialog).getByText(/Variables are placeholder values that make your prompt flexible and reusable/),
    ).toBeTruthy();
    expect(within(within(dialog).getByLabelText("Generated prompt variables")).getByText("{{topic}}")).toBeTruthy();
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    const continueButton = within(dialog).getByRole("button", { name: "Continue" }) as HTMLButtonElement;
    await waitFor(() => expect(continueButton.disabled).toBe(false));
    fireEvent.click(continueButton);

    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Generate a prompt" })).toBeNull());
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe(generatedPrompt);
    const request = api.requests.find((item) => item.url.endsWith("/workbench/generate_prompt"));
    expect(request?.method).toBe("POST");
    expect(request?.body?.task).toBe("Summarize meeting notes into action items.");
    expect(request?.body?.target_thinking_mode).toBe(true);
    expect(request?.body?.isPromptConversion).toBe(false);
    expect(request?.body?.feedback).toBeUndefined();
    const revisionIndex = api.requests.findIndex(
      (item) => item.method === "POST" && /\/workbench\/prompts\/prompt_1\/revisions$/.test(item.url),
    );
    const titleIndex = api.requests.findIndex((item) => item.url.endsWith("/workbench/generate_title"));
    const updatePromptIndex = api.requests.findIndex(
      (item) => item.method === "PUT" && /\/workbench\/prompts\/prompt_1$/.test(item.url),
    );
    expect(revisionIndex).toBeGreaterThan(-1);
    expect(titleIndex).toBeGreaterThan(revisionIndex);
    expect(updatePromptIndex).toBeGreaterThan(titleIndex);
    expect(api.requests[revisionIndex]?.body?.variables).toEqual(["topic"]);
    expect(api.requests[titleIndex]?.body).toEqual({
      message_content: generatedPrompt,
      model: "claude-opus-4-8",
    });
    expect(api.requests[updatePromptIndex]?.body).toEqual({ name: "Cat haiku" });
  });

  test("confirms before closing the prompt generator while generation is still running", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi({
      initialPromptName: "",
      initialPromptText: "",
      generatedPromptDelayMs: 1000,
      generatedPromptText: "<Instructions>Write a crisp project brief about {$topic}.</Instructions>",
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Generate Prompt" });
    fireEvent.click(screen.getByRole("button", { name: "Generate Prompt" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Describe your task..." }), {
      target: { value: "Create a project brief prompt." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Generate" }));

    const dialog = screen.getByRole("dialog", { name: "Generate a prompt" });
    expect(within(dialog).getByText("Generating prompt…")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));

    const confirmDialog = screen.getByRole("alertdialog", { name: "Close prompt generator?" });
    expect(within(confirmDialog).getByText("By closing this modal, you will lose any progress made.")).toBeTruthy();
    fireEvent.click(within(confirmDialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "Close prompt generator?" })).toBeNull());
    expect(screen.getByRole("dialog", { name: "Generate a prompt" })).toBeTruthy();

    fireEvent.click(
      within(screen.getByRole("dialog", { name: "Generate a prompt" })).getByRole("button", { name: "Close" }),
    );
    fireEvent.click(
      within(screen.getByRole("alertdialog", { name: "Close prompt generator?" })).getByRole("button", {
        name: "Close",
      }),
    );

    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Generate a prompt" })).toBeNull());
  });

  test("confirms before clearing generated prompt output when returning to the task step", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const generatedPrompt = "Write a crisp project brief about {{topic}}.";
    mockWorkbenchApi({
      initialPromptName: "",
      initialPromptText: "",
      generatedPromptDelayMs: 25,
      generatedPromptText: `<planning>Draft the reusable prompt.</planning>\n<Instructions>${generatedPrompt.replace("{{topic}}", "{$topic}")}</Instructions>`,
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Generate Prompt" });
    fireEvent.click(screen.getByRole("button", { name: "Generate Prompt" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Describe your task..." }), {
      target: { value: "Create a project brief prompt." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Generate" }));

    const dialog = screen.getByRole("dialog", { name: "Generate a prompt" });
    const output = (await within(dialog).findByRole("textbox", { name: "Your prompt" })) as HTMLTextAreaElement;
    await waitFor(() => expect(output.value).toBe(generatedPrompt));

    fireEvent.click(within(dialog).getByRole("button", { name: "Back" }));
    const confirmDialog = screen.getByRole("alertdialog", { name: "Clear generated prompt?" });
    expect(
      within(confirmDialog).getByText("Editing this page will clear the existing generated/converted prompt."),
    ).toBeTruthy();
    fireEvent.click(within(confirmDialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog", { name: "Clear generated prompt?" })).toBeNull());
    expect(
      (
        within(screen.getByRole("dialog", { name: "Generate a prompt" })).getByRole("textbox", {
          name: "Your prompt",
        }) as HTMLTextAreaElement
      ).value,
    ).toBe(generatedPrompt);

    fireEvent.click(
      within(screen.getByRole("dialog", { name: "Generate a prompt" })).getByRole("button", { name: "Back" }),
    );
    fireEvent.click(
      within(screen.getByRole("alertdialog", { name: "Clear generated prompt?" })).getByRole("button", {
        name: "Clear prompt",
      }),
    );

    await waitFor(() =>
      expect(
        within(screen.getByRole("dialog", { name: "Generate a prompt" })).queryByRole("textbox", {
          name: "Your prompt",
        }),
      ).toBeNull(),
    );
    expect(
      (
        within(screen.getByRole("dialog", { name: "Generate a prompt" })).getByRole("textbox", {
          name: "Describe your task...",
        }) as HTMLTextAreaElement
      ).value,
    ).toBe("Create a project brief prompt.");
  });

  test("smoothly previews Generate Prompt instructions while preserving cleaned raw output", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const generatedPrompt = [
      "Write a launch readiness brief about {{topic}}.",
      "Include scope, milestones, customer impact, support risks, and launch owner.",
      "Keep the final answer concise but specific enough for a product review.",
    ].join("\n");
    mockWorkbenchApi({
      initialPromptName: "",
      initialPromptText: "",
      generatedPromptText: `<planning>Draft the reusable prompt.</planning>\n<Instructions>${generatedPrompt.replace(
        "{{topic}}",
        "{$topic}",
      )}\nLet me know if you want me to adjust this.</Instructions>`,
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Generate Prompt" });
    fireEvent.click(screen.getByRole("button", { name: "Generate Prompt" }));
    const dialog = screen.getByRole("dialog", { name: "Generate a prompt" });
    fireEvent.change(within(dialog).getByRole("textbox", { name: "Describe your task..." }), {
      target: { value: "Create a launch readiness prompt." },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Generate" }));

    const generatedPromptTextarea = (await within(dialog).findByRole("textbox", {
      name: "Your prompt",
    })) as HTMLTextAreaElement;
    await waitFor(() => {
      expect(generatedPromptTextarea.value.length).toBeGreaterThan(0);
      expect(generatedPromptTextarea.value.length).toBeLessThan(generatedPrompt.length);
    });
    await waitFor(() => expect(generatedPromptTextarea.value).toBe(generatedPrompt), { timeout: 3000 });
  });

  test("selects Generate Prompt examples and expands the preset list", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi({ initialPromptText: "" });
    renderWorkbench();

    await screen.findByRole("button", { name: "Generate Prompt" });
    fireEvent.click(screen.getByRole("button", { name: "Generate Prompt" }));

    const dialog = screen.getByRole("dialog", { name: "Generate a prompt" });
    const taskInput = within(dialog).getByRole("textbox", { name: "Describe your task..." }) as HTMLTextAreaElement;
    expect(within(dialog).getByRole("button", { name: /Summarize a document/i })).toBeTruthy();
    expect(within(dialog).queryByRole("button", { name: /Recommend a product/i })).toBeNull();

    fireEvent.click(within(dialog).getByRole("button", { name: /Summarize a document/i }));
    await waitFor(() => expect(taskInput.value).toBe("Summarize documents into 10 bullet points max"));

    fireEvent.click(within(dialog).getByRole("button", { name: /Write me an email/i }));
    await waitFor(() =>
      expect(taskInput.value).toBe("Draft an email responding to a customer complaint email and offer a resolution"),
    );

    fireEvent.click(within(dialog).getByRole("button", { name: /Translate code/i }));
    await waitFor(() => expect(taskInput.value).toBe("Translate code to Python"));

    fireEvent.click(within(dialog).getByRole("button", { name: "Show more prompt examples" }));
    expect(within(dialog).getByRole("button", { name: /Recommend a product/i })).toBeTruthy();

    fireEvent.click(within(dialog).getByRole("button", { name: /Content moderation/i }));
    await waitFor(() =>
      expect(taskInput.value).toBe("Classify chat transcripts into categories using our content moderation policy"),
    );

    fireEvent.click(within(dialog).getByRole("button", { name: /Recommend a product/i }));
    await waitFor(() =>
      expect(taskInput.value).toBe("Recommend a product based on a customer’s previous transactions"),
    );
  });

  test("hides Generate Prompt as soon as the first user message receives input", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi({ initialPromptText: "" });
    renderWorkbench();

    const userPrompt = (await screen.findByRole("textbox", { name: "User prompt 1" })) as HTMLTextAreaElement;
    expect(screen.getByRole("button", { name: "Generate Prompt" })).toBeTruthy();
    fireEvent.change(userPrompt, { target: { value: "Draft a concise launch plan." } });

    await waitFor(() => expect(screen.queryByRole("button", { name: "Generate Prompt" })).toBeNull());
    expect(userPrompt.value).toBe("Draft a concise launch plan.");
  });

  test("disables Generate Prompt when Workbench prompt generation is unavailable", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi({ initialPromptText: "" });
    renderWorkbench({
      auth: authContextValue({
        ...defaultAuthAccount(),
        memberships: [
          {
            role: "developer",
            seat_tier: "enterprise_standard",
            organization: {
              uuid: "org_test",
              name: "Claude Platform",
              api_disabled_reason: "out_of_credits",
              settings: {
                product_name: "Claude Platform",
                workbench: { enabled: true },
              } as any,
            },
          },
        ],
      }),
    });

    const button = (await screen.findByRole("button", { name: "Generate Prompt" })) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(button.closest("[title]")?.getAttribute("title")).toBe("Get More credits to use the prompt generator");
    fireEvent.click(button);
    expect(screen.queryByRole("dialog", { name: "Generate a prompt" })).toBeNull();
  });

  test("shows official Improve prompt warnings for images and multi-turn prompts", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({
      initialPromptText: "Describe this image.",
      uploadedFile: { filename: "diagram.png", mime_type: "image/png" },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.change(screen.getByTestId("workbench-upload-input-0"), {
      target: { files: [new File(["image bytes"], "diagram.png", { type: "image/png" })] },
    });
    await waitFor(() => expect(api.requests.some((request) => request.url === "/v1/files?beta=true")).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Add message pair" }));
    fireEvent.click(screen.getByRole("button", { name: "Use Claude to optimize your prompt" }));

    const dialog = screen.getByRole("dialog", { name: "What would you like to improve?" });
    expect(
      within(dialog).getByText("Images will not be processed. Please manually add them back in to improved prompt."),
    ).toBeTruthy();
    expect(
      within(dialog).getByText("Multi-turn prompt detected. Only the first user message will be improved."),
    ).toBeTruthy();
  });

  test("keeps explicit Workbench prompt URLs instead of falling back to the workspace prompt list", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_custom");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });

    expect(window.location.pathname).toBe("/workbench/prompt_custom");
    expect(api.requests.some((request) => request.url.endsWith("/workbench/prompts/prompt_custom"))).toBe(true);
  });

  test("preserves direct Evaluate deep links when the prompt has variables", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    mockWorkbenchApi({ initialPromptText: "Write a haiku about {{animal}}." });
    renderWorkbench();

    expect(await screen.findByRole("button", { name: /Run All/ })).toBeTruthy();
    expect(window.location.pathname).toBe("/workbench/prompt_1");
    expect(window.location.search).toBe("?tab=evaluate");
    expect(screen.getByRole("tablist", { name: "Workbench mode" })).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Evaluate" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("switch", { name: "Show Prompt" }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByRole("switch", { name: "Show Ideal Outputs" }).getAttribute("aria-checked")).toBe("false");
    expect(document.body.textContent).toContain("{{animal}}");
    expect(document.body.textContent).toContain("Model output");
    expect(document.body.textContent).toContain("No test cases");
    expect(document.body.textContent).not.toContain("Pre-fill response");
    expect(screen.getByRole("button", { name: "Add Row" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Generate Test Case" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Import Test Cases" })).toBeTruthy();
    expect((screen.getByRole("button", { name: "Export to CSV" }) as HTMLButtonElement).disabled).toBe(true);
  });

  test("falls back from direct Evaluate deep links when the prompt uses tools", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    mockWorkbenchApi({
      initialPromptText: "Write a haiku about {{animal}}.",
      initialTools: [{ type: "web_search_v0", name: "web_search" }],
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    await waitFor(() => expect(window.location.search).toBe(""));
    const evaluateButton = screen.getByRole("tab", { name: "Evaluate" });
    expect(evaluateButton.getAttribute("aria-disabled")).toBe("true");
    expect(evaluateButton.getAttribute("title")).toBe(
      "Run a prompt with at least one variable and no tools to use ‘Evaluate’.",
    );
    expect(screen.queryByRole("button", { name: /Run All/ })).toBeNull();
  });

  test("creates a clean Workbench when loading the official new route", async () => {
    resetTestDom("https://oma.duck.ai/workbench/new?tab=evaluate");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect(window.location.search).toBe("");
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    expect(screen.getByRole("tab", { name: "Evaluate" }).getAttribute("aria-disabled")).toBe("true");
    expect((screen.getByRole("button", { name: "Run ⌘ + ⏎" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    expect(
      api.requests.filter(
        (request) => request.method === "POST" && request.url.endsWith("/workspaces/default/prompts"),
      ),
    ).toHaveLength(1);
    expect(api.requests.some((request) => request.url.endsWith("/workbench/prompts/prompt_1"))).toBe(false);
  });

  test("creates a new Workbench from the plus button and resets the editor state", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ createPromptDelayMs: 50 });
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    fireEvent.change(userPrompt, { target: { value: "Keep this only on the old Workbench." } });
    fireEvent.click(screen.getByRole("button", { name: "Model settings" }));
    fireEvent.click(screen.getByRole("combobox", { name: "claude-opus-4-8" }));
    fireEvent.click(screen.getByRole("option", { name: /claude-sonnet-4-6/i }));
    fireEvent.click(screen.getByRole("button", { name: "New prompt" }));

    expect(window.location.pathname).toBe("/workbench/new");
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    expect(screen.getByRole("tab", { name: "Evaluate" }).getAttribute("aria-disabled")).toBe("true");
    expect((screen.getByRole("button", { name: "Run ⌘ + ⏎" }) as HTMLButtonElement).disabled).toBe(true);

    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Model settings" }));
    expect(screen.getByRole("combobox", { name: "claude-opus-4-8" }).textContent).toContain("claude-opus-4-8");
    expect(document.body.textContent).toContain("Run prompt to see assistant response from the");
    expect(screen.getByText("Untitled")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Evaluate" }).getAttribute("aria-disabled")).toBe("true");
    expect((screen.getByRole("button", { name: "Requires at least one variable" }) as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(
      (screen.getByRole("button", { name: "Add some text to the prompt to use this feature" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect((screen.getByRole("button", { name: "Pre-fill response" }) as HTMLButtonElement).disabled).toBe(true);
    const createRequest = api.requests.find(
      (request) => request.method === "POST" && request.url.endsWith("/workspaces/default/prompts"),
    );
    expect(createRequest?.body).toEqual({});
    const createRequestIndex = api.requests.findIndex(
      (request) => request.method === "POST" && request.url.endsWith("/workspaces/default/prompts"),
    );
    const refreshListIndex = api.requests.findIndex(
      (request, index) =>
        index > createRequestIndex && request.method === "GET" && request.url.endsWith("/workbench/prompts"),
    );
    expect(refreshListIndex).toBeGreaterThan(createRequestIndex);
  });

  test("creates a new Workbench from the new prompt button without duplicate prompt creation", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ createPromptDelayMs: 50 });
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    fireEvent.change(userPrompt, { target: { value: "Keep this only on the old Workbench." } });

    const newPromptButton = screen.getByRole("button", { name: "New prompt" });
    fireEvent.click(newPromptButton);

    expect(window.location.pathname).toBe("/workbench/new");
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    expect(
      api.requests.filter(
        (request) => request.method === "POST" && request.url.endsWith("/workspaces/default/prompts"),
      ),
    ).toHaveLength(1);
  });

  test("new Workbench clears open drawers and variable test case state immediately", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi({ initialPromptText: "Write a haiku about {{animal}}.", createPromptDelayMs: 50 });
    renderWorkbench();

    await screen.findByLabelText("User prompt 1");
    fireEvent.click(screen.getByRole("button", { name: "Run ⌘ + ⏎" }));
    const panel = await screen.findByLabelText("Test Case");
    fireEvent.change(within(panel).getByRole("textbox", { name: "{{animal}}" }), { target: { value: "owl" } });
    expect((within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }) as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "New prompt" }));

    expect(window.location.pathname).toBe("/workbench/new");
    expect(screen.queryByLabelText("Test Case")).toBeNull();
    expect(screen.queryByRole("textbox", { name: "{{animal}}" })).toBeNull();
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
  });

  test("prevents duplicate Workbench creation while the plus button is pending", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ createPromptDelayMs: 50 });
    renderWorkbench();

    await screen.findByLabelText("User prompt 1");
    const newPromptButton = screen.getByRole("button", { name: "New prompt" }) as HTMLButtonElement;
    fireEvent.click(newPromptButton);
    fireEvent.click(newPromptButton);

    expect(window.location.pathname).toBe("/workbench/new");
    expect(newPromptButton.disabled).toBe(true);
    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    expect(
      api.requests.filter(
        (request) => request.method === "POST" && request.url.endsWith("/workspaces/default/prompts"),
      ),
    ).toHaveLength(1);
  });

  test("creates a new Workbench from Evaluate and returns to a clean Prompt page", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    mockWorkbenchApi({ initialPromptText: "Write a haiku about {{animal}}." });
    renderWorkbench();

    await screen.findByRole("button", { name: /Run All/ });
    fireEvent.click(screen.getByRole("button", { name: "New prompt" }));

    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect(window.location.search).toBe("");
    expect(screen.getByRole("button", { name: "Run ⌘ + ⏎" })).toBeTruthy();
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    expect(screen.getByRole("tab", { name: "Evaluate" }).getAttribute("aria-disabled")).toBe("true");
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    expect(document.body.textContent).toContain("Run prompt to see assistant response from the");
  });

  test("creates a clean Workbench from the official response empty-state action", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ initialPromptText: "", createPromptDelayMs: 50 });
    renderWorkbench();

    const responseCreateButton = await screen.findByRole("button", { name: "Create New Prompt" });
    fireEvent.click(responseCreateButton);

    expect(window.location.pathname).toBe("/workbench/new");
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    expect(
      api.requests.filter(
        (request) => request.method === "POST" && request.url.endsWith("/workspaces/default/prompts"),
      ),
    ).toHaveLength(1);
    expect(api.requests.some((request) => request.method === "GET" && request.url.endsWith("/workbench/prompts"))).toBe(
      true,
    );
  });

  test("adds an assistant prefill response when the prompt has user content", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    await screen.findByLabelText("User prompt 1");
    const prefillButton = screen.getByRole("button", { name: "Pre-fill response" }) as HTMLButtonElement;
    expect(prefillButton.disabled).toBe(false);

    fireEvent.click(prefillButton);
    const assistantPrompt = screen.getByLabelText("Assistant prompt 2") as HTMLTextAreaElement;
    expect(assistantPrompt).toBeTruthy();
    expect(assistantPrompt.placeholder).toBe("");
    expect((screen.getByRole("button", { name: "Pre-fill response" }) as HTMLButtonElement).disabled).toBe(true);
  });

  test("adds and deletes message pairs in the official assistant then user order", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    await screen.findByLabelText("User prompt 1");
    fireEvent.click(screen.getByRole("button", { name: "Add message pair" }));

    const assistantPrompt = screen.getByLabelText("Assistant prompt 2") as HTMLTextAreaElement;
    const userPrompt = screen.getByLabelText("User prompt 3") as HTMLTextAreaElement;
    expect(assistantPrompt.placeholder).toBe("");
    expect(userPrompt.placeholder).toBe("");
    expect(screen.queryByLabelText("User prompt 2")).toBeNull();
    expect(screen.queryByLabelText("Assistant prompt 3")).toBeNull();

    const deletePairButtons = screen.getAllByRole("button", {
      name: "Delete both to maintain user & assistant alternation",
    });
    expect(deletePairButtons).toHaveLength(2);
    fireEvent.click(deletePairButtons[1]);

    expect(screen.queryByLabelText("Assistant prompt 2")).toBeNull();
    expect(screen.queryByLabelText("User prompt 3")).toBeNull();
    expect(screen.getByLabelText("User prompt 1")).toBeTruthy();
  });

  test("renders the official-style prompt settings menu and save changes link", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });

    expect(screen.getByText(/Last saved Jun 12/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));
    const menu = screen.getByRole("menu");
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["Rename prompt", "Save", "Version history", "Make a copy", "Share", "Delete"]);
    expect(within(menu).getByRole("menuitem", { name: "Save" }).getAttribute("aria-disabled")).toBe("true");
    expect(menu.querySelector('[data-slot="dropdown-menu-separator"]')).toBeTruthy();
    fireEvent.keyDown(window, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();

    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    fireEvent.change(screen.getByLabelText("User prompt 1"), { target: { value: "Updated complaint response." } });
    const saveChanges = await screen.findByRole("button", { name: "Save changes" });
    fireEvent.click(saveChanges);
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.method === "POST" && request.url.endsWith("/workbench/prompts/prompt_1/revisions"),
        ),
      ).toBe(true),
    );
  });

  test("opens the prompt settings menu from pointer down without closing on click", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    const promptSettings = await screen.findByRole("button", { name: "Prompt settings" });
    fireEvent.pointerDown(promptSettings, { button: 0 });
    fireEvent.click(promptSettings);

    const menu = screen.getByRole("menu");
    expect(
      within(menu)
        .getAllByRole("menuitem")
        .map((item) => item.textContent),
    ).toEqual(["Rename prompt", "Save", "Version history", "Make a copy", "Share", "Delete"]);
    expect(promptSettings.getAttribute("aria-expanded")).toBe("true");
  });

  test("opens the official-style rename prompt dialog and closes the settings menu", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Rename prompt" }));

    expect(screen.queryByRole("menu")).toBeNull();
    const dialog = screen.getByRole("dialog", { name: "Rename your prompt" });
    const nameInput = within(dialog).getByRole("textbox", { name: "Name" }) as HTMLInputElement;
    expect(nameInput.value).toBe("Complaint response");
    expect(within(dialog).queryByText("Name")).toBeNull();

    fireEvent.change(nameInput, { target: { value: "Renamed prompt" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.method === "PUT" &&
            request.url.endsWith("/workbench/prompts/prompt_1") &&
            request.body?.name === "Renamed prompt",
        ),
      ).toBe(true),
    );
    const renamePutIndex = api.requests.findIndex(
      (request) => request.method === "PUT" && request.url.endsWith("/workbench/prompts/prompt_1"),
    );
    const refreshPromptIndex = api.requests.findIndex(
      (request, index) =>
        index > renamePutIndex && request.method === "GET" && request.url.endsWith("/workbench/prompts/prompt_1"),
    );
    const refreshListIndex = api.requests.findIndex(
      (request, index) =>
        index > refreshPromptIndex && request.method === "GET" && request.url.endsWith("/workbench/prompts"),
    );
    expect(refreshPromptIndex).toBeGreaterThan(renamePutIndex);
    expect(refreshListIndex).toBeGreaterThan(refreshPromptIndex);
    expect(screen.queryByRole("dialog", { name: "Rename your prompt" })).toBeNull();
    expect(screen.getByRole("button", { name: "Prompt settings" }).textContent).toContain("Renamed prompt");
  });

  test("restores a saved version from Version history", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({
      initialPromptText: "Latest prompt body.",
      revisionIds: { prompt_1: "workbench-revision-latest" },
      revisionsByPrompt: {
        prompt_1: [
          promptRevision("Latest prompt body.", "workbench-revision-latest"),
          promptRevision("Earlier prompt about {{animal}}.", "workbench-revision-earlier"),
        ],
      },
      evaluationsByRevision: {
        "workbench-revision-earlier": [
          {
            id: "eval_history",
            revision_id: "workbench-revision-earlier",
            test_case_id: "eval_history",
            variable_values: { animal: "otter" },
            golden_answer: "",
            completion_text: "Historical output.",
            rating: "",
          },
        ],
      },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Version history" }));

    expect(screen.queryByRole("menu")).toBeNull();
    const panel = screen.getByLabelText("Version history");
    expect(await within(panel).findByText("Jun 12, 2026")).toBeTruthy();
    expect(await within(panel).findAllByText("Untitled version")).toHaveLength(2);
    const currentRevision = within(panel).getByRole("button", { name: "Revision v2" });
    expect(currentRevision.getAttribute("aria-current")).toBe("true");
    expect(within(panel).queryByRole("button", { name: "Restore this version" })).toBeNull();
    expect(within(panel).queryByLabelText("Version preview")).toBeNull();
    fireEvent.click(within(panel).getByRole("button", { name: "Revision v1" }));

    await waitFor(() =>
      expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe(
        "Earlier prompt about {{animal}}.",
      ),
    );
    expect(screen.getByText("Unsaved")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "Evaluate" }));
    expect(((await screen.findByLabelText("animal row 1")) as HTMLTextAreaElement).value).toBe("otter");
    expect(
      api.requests.some(
        (request) =>
          request.method === "GET" &&
          request.url.endsWith("/workbench/prompts/prompt_1/revisions/workbench-revision-earlier"),
      ),
    ).toBe(true);
    expect(
      api.requests.some(
        (request) =>
          request.method === "GET" &&
          request.url.endsWith("/workbench/revisions/workbench-revision-earlier/evaluations/list"),
      ),
    ).toBe(true);
  });

  test("requires saving or discarding a draft before viewing previous versions", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({
      initialPromptText: "Latest prompt body.",
      revisionIds: { prompt_1: "workbench-revision-latest" },
      revisionsByPrompt: {
        prompt_1: [
          promptRevision("Latest prompt body.", "workbench-revision-latest"),
          promptRevision("Earlier prompt about {{animal}}.", "workbench-revision-earlier"),
        ],
      },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.change(screen.getByLabelText("User prompt 1"), { target: { value: "Draft-only prompt body." } });
    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));

    const menu = screen.getByRole("menu");
    expect(within(menu).getByRole("menuitem", { name: "Version history" })).toBeTruthy();
    expect(screen.queryByLabelText("Version history")).toBeNull();
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Version history" }));

    const panel = screen.getByLabelText("Version history");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(await within(panel).findByRole("button", { name: "Revision Draft version" })).toBeTruthy();
    expect(within(panel).getByText("Draft-only prompt body.")).toBeTruthy();
    expect(await within(panel).findByText("Previously")).toBeTruthy();
    expect(within(panel).getByText(/You are currently editing a draft version/)).toBeTruthy();

    const revisionDetailRequestsBeforeClick = api.requests.filter(
      (request) =>
        request.method === "GET" &&
        request.url.endsWith("/workbench/prompts/prompt_1/revisions/workbench-revision-earlier"),
    ).length;
    fireEvent.click(within(panel).getByRole("button", { name: "Revision v1" }));
    expect(
      api.requests.filter(
        (request) =>
          request.method === "GET" &&
          request.url.endsWith("/workbench/prompts/prompt_1/revisions/workbench-revision-earlier"),
      ),
    ).toHaveLength(revisionDetailRequestsBeforeClick);

    fireEvent.click(within(panel).getByRole("button", { name: "discard" }));
    await waitFor(() =>
      expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("Latest prompt body."),
    );
    expect(
      api.requests.some(
        (request) =>
          request.method === "POST" &&
          request.url.endsWith("/workbench/prompts/prompt_1/kv_store/set/draft_revision") &&
          String(request.body?.value).includes("Latest prompt body."),
      ),
    ).toBe(true);
    expect(within(panel).queryByRole("button", { name: "Revision Draft version" })).toBeNull();

    fireEvent.click(within(panel).getByRole("button", { name: "Revision v1" }));
    await waitFor(() =>
      expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe(
        "Earlier prompt about {{animal}}.",
      ),
    );
    expect(
      api.requests.some(
        (request) =>
          request.method === "GET" &&
          request.url.endsWith("/workbench/prompts/prompt_1/revisions/workbench-revision-earlier"),
      ),
    ).toBe(true);
  });

  test("confirms before deleting a Workbench prompt", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));

    const dialog = screen.getByRole("alertdialog", { name: "Delete prompt" });
    expect(within(dialog).getByText("Are you sure you want to permanently delete this prompt?")).toBeTruthy();
    expect(within(dialog).queryByRole("button", { name: "Close dialog" })).toBeNull();
    expect(api.requests.some((request) => request.method === "DELETE")).toBe(false);
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("alertdialog", { name: "Delete prompt" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }));
    fireEvent.click(
      within(screen.getByRole("alertdialog", { name: "Delete prompt" })).getByRole("button", { name: "Delete" }),
    );
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.method === "DELETE" && request.url.endsWith("/workbench/prompts/prompt_1"),
        ),
      ).toBe(true),
    );
  });

  test("opens the official-style Share Prompt dialog before sharing with the workspace", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Share" }));

    const dialog = screen.getByRole("dialog", { name: "Share Prompt" });
    expect(within(dialog).getByText("Complaint response")).toBeTruthy();
    expect(within(dialog).getByText("Jun 12, 2026 by user_default")).toBeTruthy();
    expect(within(dialog).getByRole("heading", { name: "Access" })).toBeTruthy();
    expect(
      within(dialog).getByText("Only you can access this prompt until it is shared with the workspace."),
    ).toBeTruthy();
    expect(api.requests.some((request) => request.url.endsWith("/workbench/prompts/prompt_1/sharing"))).toBe(false);

    fireEvent.click(within(dialog).getByRole("button", { name: "Share with workspace" }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.method === "POST" && request.url.endsWith("/workbench/prompts/prompt_1/sharing"),
        ),
      ).toBe(true),
    );
    expect(within(dialog).getByText("Shared with workspace")).toBeTruthy();
    expect(within(dialog).getByText("Anyone in this workspace can find and use this prompt.")).toBeTruthy();
  });

  test("shows the official read-only Share Prompt dialog for prompts created by someone else", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({
      promptCreators: {
        prompt_1: {
          tagged_id: "user_other",
          uuid: "user_other",
          full_name: "user_other",
          email_address: "user_other",
        },
      },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Share" }));

    const dialog = screen.getByRole("dialog", { name: "Share Prompt" });
    expect(
      within(dialog).getByText("Please ask the creator of this prompt, user_other, to modify its sharing settings."),
    ).toBeTruthy();
    expect(within(dialog).queryByRole("button", { name: "Share with workspace" })).toBeNull();
    expect(dialog.querySelector(".workbench-share-content.is-readonly")).toBeTruthy();
    expect(api.requests.some((request) => request.url.endsWith("/workbench/prompts/prompt_1/sharing"))).toBe(false);
  });

  test("makes a prompt copy with the official single create request", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Prompt settings" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Make a copy" }));

    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect(screen.getByRole("button", { name: "Prompt settings" }).textContent).toContain("Complaint response copy");
    const createRequest = api.requests.find(
      (request) => request.method === "POST" && request.url.endsWith("/workspaces/default/prompts"),
    );
    expect(createRequest?.body?.name).toBe("Complaint response copy");
    expect(createRequest?.body?.latest_revision?.messages?.[0]?.content?.[0]?.text).toContain(
      "Draft an email responding",
    );
    expect(api.requests.some((request) => request.method === "GET" && request.url.endsWith("/workbench/prompts"))).toBe(
      true,
    );
    expect(
      api.requests.some(
        (request) => request.method === "PUT" && request.url.endsWith("/workbench/prompts/prompt_new_1"),
      ),
    ).toBe(false);
    expect(
      api.requests.some(
        (request) => request.method === "POST" && request.url.endsWith("/workbench/prompts/prompt_new_1/revisions"),
      ),
    ).toBe(false);
  });

  test("opens the official-style prompt picker with search, ownership toggle, and create action", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi({
      promptSummaries: [
        {
          id: "prompt_1",
          name: "Complaint response",
          workspace_id: "default",
          creator: defaultPromptCreator(),
        },
        {
          id: "prompt_other",
          name: "Other author prompt",
          workspace_id: "default",
          creator: { tagged_id: "user_other" },
        },
      ],
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Open prompt list" }));

    const picker = screen.getByRole("dialog", { name: "Prompts" });
    expect(picker.getAttribute("data-slot")).toBe("popover-content");
    expect(within(picker).getByRole("combobox", { name: "Search prompts" })).toBeTruthy();
    expect(within(picker).getByRole("option", { name: /Other author prompt/i })).toBeTruthy();
    expect(within(picker).getByText("by user_other")).toBeTruthy();
    const ownershipToggle = within(picker).getByRole("switch", { name: "Only show my prompts" });
    expect(ownershipToggle.getAttribute("aria-checked")).toBe("false");
    fireEvent.click(ownershipToggle);
    expect(ownershipToggle.getAttribute("aria-checked")).toBe("true");
    expect(within(picker).queryByRole("option", { name: /Other author prompt/i })).toBeNull();

    fireEvent.change(within(picker).getByRole("combobox", { name: "Search prompts" }), {
      target: { value: "missing" },
    });
    expect(within(picker).getByText("No prompts found")).toBeTruthy();
    fireEvent.change(within(picker).getByRole("combobox", { name: "Search prompts" }), {
      target: { value: "complaint" },
    });
    expect(within(picker).getByRole("option", { name: /Complaint response/i })).toBeTruthy();
    fireEvent.click(within(picker).getByRole("button", { name: "Delete prompt" }));
    expect(screen.getByRole("alertdialog", { name: "Delete prompt" })).toBeTruthy();
    fireEvent.click(
      within(screen.getByRole("alertdialog", { name: "Delete prompt" })).getByRole("button", { name: "Cancel" }),
    );
    expect(screen.queryByRole("alertdialog", { name: "Delete prompt" })).toBeNull();

    if (!screen.queryByRole("dialog", { name: "Prompts" })) {
      fireEvent.click(screen.getByRole("button", { name: "Open prompt list" }));
    }
    fireEvent.click(
      within(screen.getByRole("dialog", { name: "Prompts" })).getByRole("button", { name: "Create New Prompt" }),
    );
    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
  });

  test("closes the shared prompt picker on outside click", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Open prompt list" }));

    expect(screen.getByRole("dialog", { name: "Prompts" })).toBeTruthy();
    expect(screen.queryByTestId("workbench-prompt-picker-backdrop")).toBeNull();

    await act(async () => {
      fireEvent.pointerDown(document.body);
      fireEvent.click(document.body);
    });

    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Prompts" })).toBeNull());
  });

  test("shows the current draft title and hides stale untitled prompts in the prompt picker", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1");
    mockWorkbenchApi({
      initialPromptName: "",
      initialPromptText: "Write a haiku about {{ANIMAL}}.",
      promptSummaries: [
        {
          id: "prompt_1",
          name: "",
          workspace_id: "default",
          created_at: "2026-06-18T06:33:01.369Z",
          updated_at: "2026-06-23T00:02:51.764Z",
        },
        {
          id: "stale_blank",
          name: "",
          workspace_id: "default",
          created_at: "2026-06-23T01:32:14.739Z",
          updated_at: "2026-06-23T01:32:14.774Z",
        },
        {
          id: "named_prompt",
          name: "Named prompt",
          workspace_id: "default",
          created_at: "2026-06-11T02:10:24.382Z",
          updated_at: "2026-06-20T02:10:24.382Z",
        },
      ],
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Open prompt list" }));
    const picker = screen.getByRole("dialog", { name: "Prompts" });

    expect(within(picker).getByRole("option", { name: /Write a haiku about \{\{ANIMAL\}\}/ })).toBeTruthy();
    expect(within(picker).getByRole("option", { name: /Named prompt/ })).toBeTruthy();
    expect(within(picker).queryByRole("option", { name: /Untitled/ })).toBeNull();
    expect(within(picker).getByText("Jun 11, 2026 by you")).toBeTruthy();
  });

  test("deletes a non-current prompt from the official-style prompt picker row action", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1");
    const api = mockWorkbenchApi({
      promptSummaries: [
        { id: "prompt_1", name: "Complaint response", workspace_id: "default" },
        { id: "prompt_2", name: "Saved evaluation prompt", workspace_id: "default" },
      ],
      promptTexts: {
        prompt_1: "Draft a complaint response.",
        prompt_2: "Evaluate this saved prompt.",
      },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.click(screen.getByRole("button", { name: "Open prompt list" }));
    const picker = screen.getByRole("dialog", { name: "Prompts" });
    fireEvent.change(within(picker).getByRole("combobox", { name: "Search prompts" }), { target: { value: "saved" } });
    expect(within(picker).getByRole("option", { name: /Saved evaluation prompt/i })).toBeTruthy();
    fireEvent.click(within(picker).getByRole("button", { name: "Delete prompt" }));

    const dialog = screen.getByRole("alertdialog", { name: "Delete prompt" });
    expect(within(dialog).getByText("Are you sure you want to permanently delete this prompt?")).toBeTruthy();
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.method === "DELETE" && request.url.endsWith("/workbench/prompts/prompt_2"),
        ),
      ).toBe(true),
    );
    expect(window.location.pathname).toBe("/workbench/prompt_1");
    if (!screen.queryByRole("dialog", { name: "Prompts" })) {
      fireEvent.click(screen.getByRole("button", { name: "Open prompt list" }));
    }
    await waitFor(() =>
      expect(
        within(screen.getByRole("dialog", { name: "Prompts" })).queryByRole("option", {
          name: /Saved evaluation prompt/i,
        }),
      ).toBeNull(),
    );
  });

  test("creates a clean Workbench from the prompt picker create button", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ createPromptDelayMs: 50 });
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    fireEvent.change(userPrompt, { target: { value: "Do not carry this into the next Workbench." } });
    fireEvent.click(screen.getByRole("button", { name: "Open prompt list" }));

    const picker = screen.getByRole("dialog", { name: "Prompts" });
    const createButton = within(picker).getByRole("button", { name: "Create New Prompt" });
    fireEvent.click(createButton);

    expect(window.location.pathname).toBe("/workbench/new");
    expect((screen.getByLabelText("User prompt 1") as HTMLTextAreaElement).value).toBe("");
    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_new_1"));
    expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
    expect(
      api.requests.filter(
        (request) => request.method === "POST" && request.url.endsWith("/workspaces/default/prompts"),
      ),
    ).toHaveLength(1);
  });

  test("restores saved Evaluate rows when selecting another prompt from the picker", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    const api = mockWorkbenchApi({
      promptSummaries: [
        { id: "prompt_1", name: "Animal haiku", workspace_id: "default" },
        { id: "prompt_2", name: "Saved evaluation prompt", workspace_id: "default" },
      ],
      promptTexts: {
        prompt_1: "Write a haiku about {{animal}}.",
        prompt_2: "Write a field note about {{animal}}.",
      },
      revisionIds: {
        prompt_1: "workbench-revision-prompt-1",
        prompt_2: "workbench-revision-prompt-2",
      },
      evaluationsByRevision: {
        "workbench-revision-prompt-2": [
          {
            id: "eval_saved_2",
            test_case_id: "test_case_saved_2",
            variable_values: { animal: "lynx" },
            golden_answer: "A careful lynx field note.",
            completion_text: "Saved model output.",
          },
        ],
      },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: /Run All/ });
    fireEvent.click(screen.getByRole("button", { name: "Open prompt list" }));
    fireEvent.click(screen.getByRole("option", { name: /Saved evaluation prompt/i }));

    await waitFor(() => expect(window.location.pathname).toBe("/workbench/prompt_2"));
    expect(window.location.search).toBe("?tab=evaluate");
    expect(((await screen.findByLabelText("animal row 1")) as HTMLTextAreaElement).value).toBe("lynx");
    expect(document.body.textContent).toContain("Saved model output.");
    expect(
      api.requests.some((request) =>
        request.url.endsWith("/workbench/revisions/workbench-revision-prompt-2/evaluations/list"),
      ),
    ).toBe(true);
  });

  test("enables the Evaluate tab only when a prompt has variables", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    expect(screen.getByRole("tablist", { name: "Workbench mode" })).toBeTruthy();
    const evaluateButton = screen.getByRole("tab", { name: "Evaluate" });
    expect(evaluateButton.getAttribute("aria-disabled")).toBe("true");

    fireEvent.change(userPrompt, { target: { value: "Write a haiku about {{animal}}." } });

    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Evaluate" }).getAttribute("aria-disabled")).not.toBe("true"),
    );
    fireEvent.click(screen.getByRole("button", { name: "Add value for animal" }));
    const variablePanel = screen.getByLabelText("Test Case");
    expect(within(variablePanel).getByRole("textbox", { name: "{{animal}}" })).toBeTruthy();
    fireEvent.click(within(variablePanel).getByRole("button", { name: "Close" }));
    fireEvent.click(screen.getByRole("tab", { name: "Evaluate" }));

    expect(window.location.search).toBe("?tab=evaluate");
    expect(screen.getByRole("tab", { name: "Evaluate" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("button", { name: /Run All/ })).toBeTruthy();
    expect(screen.getByRole("switch", { name: "Show Prompt" })).toBeTruthy();
    expect(screen.getByRole("switch", { name: "Show Ideal Outputs" })).toBeTruthy();
    expect(document.body.textContent).toContain("No test cases");
    expect(document.body.textContent).toContain("{{animal}}");
    expect(document.body.textContent).toContain("Model output");
    expect(document.body.textContent).not.toContain("Pre-fill response");
    expect(screen.getByRole("button", { name: "Add Row" })).toBeTruthy();
  });

  test("supports editable Evaluate rows, visible prompt and ideal output columns", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    const api = mockWorkbenchApi({ initialPromptText: "Write a haiku about {{animal}}." });
    renderWorkbench();

    await screen.findByRole("button", { name: /Run All/ });
    const showPrompt = screen.getByRole("switch", { name: "Show Prompt" });
    const showIdealOutputs = screen.getByRole("switch", { name: "Show Ideal Outputs" });
    fireEvent.click(showPrompt);
    fireEvent.click(showIdealOutputs);

    expect(showPrompt.getAttribute("aria-checked")).toBe("true");
    expect(showIdealOutputs.getAttribute("aria-checked")).toBe("true");
    expect(document.body.textContent).toContain("Prompt");
    expect(document.body.textContent).toContain("Ideal output");

    fireEvent.click(screen.getByRole("button", { name: "Add Row" }));
    const animalInput = screen.getByLabelText("animal row 1") as HTMLTextAreaElement;
    const idealInput = screen.getByLabelText("Ideal output row 1") as HTMLTextAreaElement;
    expect(animalInput.placeholder).toBe("Enter an example value...");
    expect(idealInput.placeholder).toBe("Enter ideal output...");
    fireEvent.change(animalInput, { target: { value: "otter" } });
    fireEvent.change(idealInput, { target: { value: "A river poem." } });
    expect(animalInput.value).toBe("otter");
    expect(idealInput.value).toBe("A river poem.");
    expect((screen.getByRole("button", { name: "Export to CSV" }) as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "Generate test case options" }));
    expect(screen.getByRole("menuitem", { name: "Generate 5 test cases" })).toBeTruthy();
    fireEvent.click(screen.getByRole("menuitem", { name: "Generate 5 test cases" }));
    await waitFor(() =>
      expect((screen.getByLabelText("animal row 6") as HTMLTextAreaElement).value).toBe("generated animal 5"),
    );
    const generateRequest = api.requests.find((request) =>
      request.url.endsWith("/workbench/metaprompt/generate_test_cases"),
    );
    expect(generateRequest?.method).toBe("POST");
    expect(generateRequest?.body?.num_testcases).toBe(5);
    expect(generateRequest?.body?.prompt).toBe("Write a haiku about {{animal}}.");
    expect(generateRequest?.body?.variables).toEqual(["animal"]);

    fireEvent.click(screen.getByRole("button", { name: "Delete row 6" }));
    expect(screen.queryByLabelText("animal row 6")).toBeNull();
  });

  test("runs all Evaluate rows through Workbench completions and writes model output", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    const api = mockWorkbenchApi({ initialPromptText: "Write a haiku about {{animal}}." });
    renderWorkbench();

    const runAll = (await screen.findByRole("button", { name: /Run All/ })) as HTMLButtonElement;
    expect(runAll.disabled).toBe(true);

    fireEvent.click(screen.getByRole("button", { name: "Add Row" }));
    expect(runAll.disabled).toBe(false);
    fireEvent.change(screen.getByLabelText("animal row 1"), { target: { value: "otter" } });
    fireEvent.click(screen.getByRole("button", { name: /Run All/ }));

    await waitFor(() =>
      expect(api.requests.map((request) => `${request.method} ${request.url}`).join("\n")).toContain(
        "/workbench/completions",
      ),
    );
    await waitFor(() => expect(document.body.textContent).toContain("Drafted response."));
    const completionRequests = api.requests.filter((request) => request.url.endsWith("/workbench/completions"));
    expect(completionRequests).toHaveLength(1);
    expect(completionRequests[0]?.body?.variable_values).toBeUndefined();
    expect(completionRequests[0]?.body?.messages?.[0]?.content).toEqual([
      { type: "text", text: "Write a haiku about ", cache_control: { type: "ephemeral" } },
      { type: "text", text: "otter" },
      { type: "text", text: "." },
    ]);
    expect(api.requests.some((request) => request.url.endsWith("/prepaid/credits"))).toBe(true);
    expect(api.requests.some((request) => request.url.includes("/evaluations/list"))).toBe(true);
    expect(
      api.requests.some((request) => request.url.endsWith("/workbench/prompts/prompt_1/revisions?compact=true")),
    ).toBe(true);
    const saveCompletionRequest = api.requests.find((request) => request.url.includes("/save_completion"));
    expect(saveCompletionRequest?.body?.completion_text).toBe("Drafted response.");
  });

  test("adds an Evaluate comparison version and runs both versions", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    const api = mockWorkbenchApi({
      initialPromptText: "Write a haiku about {{animal}}.",
      revisionIds: { prompt_1: "workbench-revision-latest" },
      revisionsByPrompt: {
        prompt_1: [
          promptRevision("Write a haiku about {{animal}}.", "workbench-revision-latest"),
          promptRevision("Compare {{animal}} carefully.", "workbench-revision-earlier"),
        ],
      },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: /Run All/ });
    fireEvent.click(screen.getByRole("button", { name: "Add Row" }));
    fireEvent.change(screen.getByLabelText("animal row 1"), { target: { value: "otter" } });
    fireEvent.click(screen.getByRole("button", { name: "Add comparison" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Version 1/ }));

    expect(screen.getByRole("button", { name: "Remove v1 comparison" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Run All/ }));

    await waitFor(() =>
      expect(api.requests.filter((request) => request.url.endsWith("/workbench/completions"))).toHaveLength(2),
    );
    const completionRequests = api.requests.filter((request) => request.url.endsWith("/workbench/completions"));
    expect(completionRequests[0]?.body?.messages?.[0]?.content).toEqual([
      { type: "text", text: "Write a haiku about ", cache_control: { type: "ephemeral" } },
      { type: "text", text: "otter" },
      { type: "text", text: "." },
    ]);
    expect(completionRequests[1]?.body?.messages?.[0]?.content).toEqual([
      { type: "text", text: "Compare ", cache_control: { type: "ephemeral" } },
      { type: "text", text: "otter" },
      { type: "text", text: " carefully." },
    ]);
    await waitFor(() =>
      expect(document.body.textContent?.match(/Drafted response\./g)?.length ?? 0).toBeGreaterThanOrEqual(2),
    );
  });

  test("keeps Evaluate read-only until prompt changes are saved as a new revision", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    const api = mockWorkbenchApi({
      initialPromptText: "Write a haiku about {{animal}}.",
      evaluationsByRevision: {
        "workbench-revision-test": [
          {
            id: "eval_existing",
            revision_id: "workbench-revision-test",
            test_case_id: "eval_existing",
            variable_values: { animal: "owl" },
            golden_answer: "",
            completion_text: "",
            rating: "",
          },
        ],
      },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: /Run All/ });
    fireEvent.click(screen.getByRole("tab", { name: "Prompt" }));
    fireEvent.change(screen.getByLabelText("User prompt 1"), {
      target: { value: "Write a crisp haiku about {{animal}}." },
    });
    fireEvent.click(screen.getByRole("tab", { name: "Evaluate" }));

    expect(screen.getByText(/evaluation table below has become stale/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Save Changes as v2" })).toBeTruthy();
    expect((screen.getByRole("button", { name: /Run All/ }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Add comparison" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Add Row" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByLabelText("animal row 1") as HTMLTextAreaElement).disabled).toBe(true);

    await waitFor(
      () => expect(api.requests.some((request) => request.url.endsWith("/kv_store/set/draft_revision"))).toBe(true),
      { timeout: 1600 },
    );
    expect(screen.getByRole("button", { name: "Save changes" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Save Changes as v2" }));
    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.method === "POST" && request.url.endsWith("/workbench/prompts/prompt_1/revisions"),
        ),
      ).toBe(true),
    );
    expect(screen.queryByText(/evaluation table below has become stale/)).toBeNull();
    expect((screen.getByRole("button", { name: /Run All/ }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Add Row" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByLabelText("animal row 1") as HTMLTextAreaElement).disabled).toBe(false);
  });

  test("persists Evaluate row create, edits, and delete through Workbench evaluation endpoints", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    const api = mockWorkbenchApi({ initialPromptText: "Write a haiku about {{animal}}." });
    renderWorkbench();

    await screen.findByRole("button", { name: /Run All/ });
    fireEvent.click(screen.getByRole("switch", { name: "Show Ideal Outputs" }));
    fireEvent.click(screen.getByRole("button", { name: "Add Row" }));

    const animalInput = screen.getByLabelText("animal row 1") as HTMLTextAreaElement;
    const idealInput = screen.getByLabelText("Ideal output row 1") as HTMLTextAreaElement;
    await waitFor(() => expect(api.requests.some((request) => request.url.endsWith("/evaluations/create"))).toBe(true));
    fireEvent.change(animalInput, { target: { value: "badger" } });
    fireEvent.change(idealInput, { target: { value: "A careful animal poem." } });
    await waitFor(() =>
      expect(api.requests.some((request) => request.url.includes("/update_golden_answer"))).toBe(true),
    );
    fireEvent.change(idealInput, { target: { value: "" } });
    expect(idealInput.value).toBe("");
    fireEvent.click(screen.getByRole("button", { name: "Delete row 1" }));

    await waitFor(() => expect(api.requests.some((request) => request.url.includes("/update_variables"))).toBe(true));
    const createRequest = api.requests.find((request) => request.url.endsWith("/evaluations/create"));
    const updateVariablesRequest = api.requests.find((request) => request.url.includes("/update_variables"));
    const updateGoldenAnswerRequests = api.requests.filter((request) => request.url.includes("/update_golden_answer"));
    const deleteRequest = api.requests.find((request) => request.url.includes("/delete"));
    expect(createRequest?.body?.variable_values).toEqual({ animal: "" });
    expect(updateVariablesRequest?.body).toEqual({ variable_values: { animal: "badger" } });
    expect(updateGoldenAnswerRequests.at(-1)?.body).toEqual({ golden_answer: "" });
    expect(deleteRequest?.method).toBe("POST");
    expect(screen.queryByLabelText("animal row 1")).toBeNull();
  });

  test("imports and exports Evaluate test cases as CSV", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1?tab=evaluate");
    mockWorkbenchApi({ initialPromptText: "Write a haiku about {{animal}}." });
    const createObjectURL = mock((_blob: Blob) => "blob:https://oma.duck.ai/workbench-cases");
    const revokeObjectURL = mock((_url: string) => undefined);
    Object.defineProperty(URL, "createObjectURL", { value: createObjectURL, configurable: true });
    Object.defineProperty(URL, "revokeObjectURL", { value: revokeObjectURL, configurable: true });
    renderWorkbench();

    await screen.findByRole("button", { name: /Run All/ });
    fireEvent.change(screen.getByTestId("workbench-evaluate-import-input"), {
      target: {
        files: [
          new File(['{{animal}},ideal_output\n"red panda","A bright forest haiku"\n'], "cases.csv", {
            type: "text/csv",
          }),
        ],
      },
    });

    await waitFor(() => expect((screen.getByLabelText("animal row 1") as HTMLTextAreaElement).value).toBe("red panda"));
    expect(screen.getByLabelText("Ideal output row 1")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Export to CSV" }));

    expect(createObjectURL).toHaveBeenCalledTimes(1);
    const exportedBlob = createObjectURL.mock.calls[0][0] as Blob;
    expect(await exportedBlob.text()).toBe("{{animal}},ideal_output\nred panda,A bright forest haiku\n");
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:https://oma.duck.ai/workbench-cases");
  });

  test("opens Test Case instead of running when variables are missing", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    fireEvent.change(userPrompt, { target: { value: "Write a haiku about {{animal}}." } });
    fireEvent.keyDown(window, { key: "Enter", metaKey: true });

    const panel = await screen.findByLabelText("Test Case");
    expect(within(panel).getByRole("heading", { name: "Test Case" })).toBeTruthy();
    expect(screen.getByRole("button", { name: /Variables/i }).className).toContain("is-active");
    expect(within(panel).getByRole("button", { name: "Generate" })).toBeTruthy();
    expect(within(panel).getByText("{{animal}}")).toBeTruthy();
    expect(within(panel).getByRole("textbox", { name: "{{animal}}" }).getAttribute("aria-invalid")).toBe("true");
    expect((within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }) as HTMLButtonElement).disabled).toBe(true);
    expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(false);
  });

  test("expands variable generation logic and sends it with generated Test Case requests", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    fireEvent.change(userPrompt, { target: { value: "Write a haiku about {{animal}}." } });
    fireEvent.click(screen.getByRole("button", { name: /Variables/i }));

    const panel = await screen.findByLabelText("Test Case");
    expect(within(panel).getByText("Variable generation logic")).toBeTruthy();
    fireEvent.click(within(panel).getByText("Variable generation logic"));

    const logicInput = within(panel).getByRole("textbox", {
      name: "Click Generate to populate with some initial logic...",
    }) as HTMLTextAreaElement;
    expect(logicInput.placeholder).toBe("Click Generate to populate with some initial logic...");
    fireEvent.click(within(panel).getByRole("button", { name: "Regenerate variable generation logic" }));
    await waitFor(() => expect(logicInput.value).toBe("Generated local Workbench test case."));

    fireEvent.change(logicInput, { target: { value: "Use a rare nocturnal animal as the generated value." } });
    fireEvent.click(within(panel).getByRole("button", { name: "Generate" }));

    await waitFor(() =>
      expect(
        api.requests.filter((request) => request.url.endsWith("/workbench/evaluations/generate_test_case")),
      ).toHaveLength(2),
    );
    const generateRequest = api.requests
      .filter((request) => request.url.endsWith("/workbench/evaluations/generate_test_case"))
      .at(-1);
    expect(generateRequest?.body?.custom_chain_of_thought).toBe("Use a rare nocturnal animal as the generated value.");
    expect(generateRequest?.body?.existing_examples).toEqual([]);
    expect((within(panel).getByRole("textbox", { name: "{{animal}}" }) as HTMLTextAreaElement).value).toBe("owl");

    fireEvent.click(within(panel).getByRole("button", { name: "Delete variable generation logic" }));
    expect(
      within(panel).queryByRole("textbox", { name: "Click Generate to populate with some initial logic..." }),
    ).toBeNull();
  });

  test("runs a prompt through the official Workbench completions request and refreshes dependent data", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Run ⌘ + ⏎" });
    fireEvent.keyDown(window, { key: "Enter", metaKey: true });

    await waitFor(() => expect(document.body.textContent).toContain("Drafted response."));
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.method).toBe("POST");
    expect(runRequest?.headers["x-organization-uuid"]).toBe("org_test");
    expect(runRequest?.headers["x-workspace-id"]).toBe("default");
    expect(runRequest?.body?.model_name).toBe("claude-opus-4-8");
    expect(runRequest?.body?.max_tokens_to_sample).toBe(20000);
    expect(runRequest?.body?.thinking).toEqual({ type: "enabled", effort: "high", budget_tokens: 16000 });
    expect(runRequest?.body?.messages).toHaveLength(1);
    expect(runRequest?.body?.messages?.[0]?.role).toBe("human");
    expect(runRequest?.body?.messages?.[0]?.content?.[0]?.type).toBe("text");
    expect(runRequest?.body?.messages?.[0]?.content?.[0]?.text).toContain("You are Claude, an expert assistant.");
    expect(api.requests.some((request) => request.url.endsWith("/prepaid/credits"))).toBe(true);
    expect(api.requests.some((request) => request.url.includes("/evaluations/list"))).toBe(true);
    expect(
      api.requests.some((request) => request.url.endsWith("/workbench/prompts/prompt_1/revisions?compact=true")),
    ).toBe(true);
  });

  test("renders the standardized response error alert when a run fails", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ completionErrorMessage: "Model request failed." });
    renderWorkbench();

    await screen.findByRole("button", { name: "Run ⌘ + ⏎" });
    fireEvent.keyDown(window, { key: "Enter", metaKey: true });

    const alert = await screen.findByRole("alert");
    expect(alert.getAttribute("data-slot")).toBe("alert");
    expect(alert.textContent).toContain("Request failed");
    expect(alert.textContent).toContain("Model request failed.");
    expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true);
  });

  test("sends the official extended thinking effort value from Model settings", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Run ⌘ + ⏎" });
    fireEvent.click(screen.getByRole("button", { name: "Model settings" }));
    const panel = screen.getByLabelText("Model");
    fireEvent.click(within(panel).getByRole("combobox", { name: "Effort" }));
    await screen.findByRole("option", { name: "Extra high" });
    selectOption("Extra high");
    fireEvent.click(within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() => expect(document.body.textContent).toContain("Drafted response."));
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.thinking).toEqual({ type: "enabled", effort: "extra_high", budget_tokens: 16000 });
  });

  test("shows Temperature when Thinking is disabled and sends it in the run payload", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Run ⌘ + ⏎" });
    fireEvent.click(screen.getByRole("button", { name: "Model settings" }));
    const panel = screen.getByLabelText("Model");
    fireEvent.click(within(panel).getByRole("radio", { name: "Disabled" }));
    const temperatureSlider = within(panel).getByRole("slider", { name: "Temperature" }) as HTMLInputElement;
    fireEvent.change(temperatureSlider, { target: { value: "0.7" } });
    fireEvent.click(within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() => expect(document.body.textContent).toContain("Drafted response."));
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.temperature).toBe(0.7);
    expect(runRequest?.body?.thinking).toEqual({ type: "disabled" });
  });

  test("shows Budget tokens when Thinking is enabled and sends it in the run payload", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    await screen.findByRole("button", { name: "Run ⌘ + ⏎" });
    fireEvent.click(screen.getByRole("button", { name: "Model settings" }));
    const panel = screen.getByLabelText("Model");
    fireEvent.click(within(panel).getByRole("radio", { name: "Enabled" }));
    const budgetSlider = within(panel).getByRole("slider", { name: "Budget tokens" }) as HTMLInputElement;
    expect(budgetSlider.value).toBe("16000");
    fireEvent.change(budgetSlider, { target: { value: "24000" } });
    fireEvent.click(within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() => expect(document.body.textContent).toContain("Drafted response."));
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.thinking).toEqual({ type: "enabled", effort: "high", budget_tokens: 24000 });

    fireEvent.click(within(panel).getByRole("radio", { name: "Adaptive" }));
    expect(within(panel).queryByRole("slider", { name: "Budget tokens" })).toBeNull();
    fireEvent.click(within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.filter((request) => request.url.endsWith("/workbench/completions"))).toHaveLength(2),
    );
    const adaptiveRequest = api.requests.filter((request) => request.url.endsWith("/workbench/completions")).at(-1);
    expect(adaptiveRequest?.body?.thinking).toEqual({ type: "adaptive", effort: "high" });
    expect(adaptiveRequest?.body?.thinking).not.toHaveProperty("budget_tokens");
  });

  test("replaces Workbench variables in the completion payload", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi();
    renderWorkbench();

    const userPrompt = (await screen.findByLabelText("User prompt 1")) as HTMLTextAreaElement;
    fireEvent.change(userPrompt, { target: { value: "Write a polished note about {{animal}}." } });
    fireEvent.click(screen.getByRole("button", { name: /Variables/i }));

    const drawer = await screen.findByRole("heading", { name: "Test Case" });
    expect(drawer).toBeTruthy();
    const panel = screen.getByLabelText("Test Case");
    expect(within(panel).getByRole("button", { name: "Generate" })).toBeTruthy();
    expect(within(panel).getByText("{{animal}}")).toBeTruthy();
    expect(within(panel).getByPlaceholderText("Enter an example value…")).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "Run ⌘ + ⏎" })).toBeTruthy();
    fireEvent.change(within(panel).getByLabelText("{{animal}}"), { target: { value: "red panda" } });
    const headerRun = document.querySelector(
      '.workbench-header-actions button[aria-label="Run ⌘ + ⏎"]',
    ) as HTMLButtonElement;
    fireEvent.click(headerRun);

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.variables).toEqual(["animal"]);
    expect(runRequest?.body?.variable_values).toBeUndefined();
    expect(runRequest?.body?.messages?.[0]?.content).toEqual([
      { type: "text", text: "Write a polished note about ", cache_control: { type: "ephemeral" } },
      { type: "text", text: "red panda" },
      { type: "text", text: "." },
    ]);
  });

  test("prepends Examples to the official Workbench completion payload", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const api = mockWorkbenchApi({ initialPromptText: "Write a haiku about {{animal}}." });
    renderWorkbench();

    await screen.findByRole("button", { name: "Run ⌘ + ⏎" });
    fireEvent.click(screen.getByRole("button", { name: "Help Claude understand the task better" }));
    const examplesPanel = screen.getByLabelText("Examples");
    fireEvent.click(within(examplesPanel).getByRole("button", { name: "Add example" }));
    fireEvent.change(within(examplesPanel).getByRole("textbox", { name: "{{animal}}" }), {
      target: { value: "falcon" },
    });
    fireEvent.change(within(examplesPanel).getByRole("textbox", { name: "Ideal output" }), {
      target: { value: "A swift sky poem." },
    });
    fireEvent.click(within(examplesPanel).getByRole("button", { name: "Add additional context" }));
    fireEvent.change(within(examplesPanel).getByRole("textbox", { name: "Additional context" }), {
      target: { value: "Keep the tone playful." },
    });
    fireEvent.click(within(examplesPanel).getByRole("button", { name: "Add Example" }));

    fireEvent.click(screen.getByRole("button", { name: /Variables/i }));
    const variablesPanel = screen.getByLabelText("Test Case");
    fireEvent.change(within(variablesPanel).getByLabelText("{{animal}}"), { target: { value: "owl" } });
    fireEvent.click(within(variablesPanel).getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    const runRequest = api.requests.find((request) => request.url.endsWith("/workbench/completions"));
    expect(runRequest?.body?.messages?.[0]?.content).toEqual([
      {
        type: "text",
        text: "<examples>\n<example>\n<example_description>\nKeep the tone playful.\n</example_description>\n<animal>\nfalcon\n</animal>\n<ideal_output>\nA swift sky poem.\n</ideal_output>\n</example>\n</examples>\n\n",
      },
      { type: "text", text: "Write a haiku about ", cache_control: { type: "ephemeral" } },
      { type: "text", text: "owl" },
      { type: "text", text: "." },
    ]);
  });

  test("generates a title before running an untitled Workbench prompt", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1");
    const api = mockWorkbenchApi({ initialPromptName: "", initialPromptText: "Write a haiku about {{ANIMAL}}." });
    renderWorkbench();

    await screen.findByRole("button", { name: "Run ⌘ + ⏎" });
    fireEvent.click(screen.getByRole("button", { name: /Variables/i }));
    const panel = await screen.findByLabelText("Test Case");
    fireEvent.change(within(panel).getByLabelText("{{ANIMAL}}"), { target: { value: "cats" } });
    fireEvent.click(within(panel).getByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    const titleIndex = api.requests.findIndex((request) => request.url.endsWith("/workbench/generate_title"));
    const updatePromptIndex = api.requests.findIndex(
      (request) => request.method === "PUT" && /\/workbench\/prompts\/prompt_1$/.test(request.url),
    );
    const completionIndex = api.requests.findIndex((request) => request.url.endsWith("/workbench/completions"));
    expect(titleIndex).toBeGreaterThan(-1);
    expect(updatePromptIndex).toBeGreaterThan(titleIndex);
    expect(updatePromptIndex).toBeLessThan(completionIndex);
    expect(api.requests[titleIndex]?.body).toEqual({
      message_content: "Write a haiku about {{ANIMAL}}.",
      model: "claude-opus-4-8",
    });
    expect(api.requests[updatePromptIndex]?.body).toEqual({ name: "Cat haiku" });
    expect(api.requests[completionIndex]?.body?.messages?.[0]?.content).toEqual([
      { type: "text", text: "Write a haiku about ", cache_control: { type: "ephemeral" } },
      { type: "text", text: "cats" },
      { type: "text", text: "." },
    ]);
  });

  test("skips automatic title generation for named Workbench prompts", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1");
    const api = mockWorkbenchApi({ initialPromptName: "Named prompt", initialPromptText: "Write a haiku about cats." });
    renderWorkbench();

    fireEvent.click(await screen.findByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/completions"))).toBe(true),
    );
    expect(api.requests.some((request) => request.url.endsWith("/workbench/generate_title"))).toBe(false);
    expect(
      api.requests.some((request) => request.method === "PUT" && /\/workbench\/prompts\/prompt_1$/.test(request.url)),
    ).toBe(false);
  });

  test("truncates the first prompt message before automatic title generation", async () => {
    resetTestDom("https://oma.duck.ai/workbench/prompt_1");
    const longPrompt = `${"A".repeat(260)} middle ${"Z".repeat(260)}`;
    const api = mockWorkbenchApi({ initialPromptName: "", initialPromptText: longPrompt });
    renderWorkbench();

    fireEvent.click(await screen.findByRole("button", { name: "Run ⌘ + ⏎" }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url.endsWith("/workbench/generate_title"))).toBe(true),
    );
    const titleRequest = api.requests.find((request) => request.url.endsWith("/workbench/generate_title"));
    expect(titleRequest?.body?.message_content).toBe(
      `${longPrompt.slice(0, 250)}\n\n  [...]\n\n  ${longPrompt.slice(-250)}`,
    );
  });

  test("opens the generated Claude API code modal with Highlight.js markup", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    const unorderedRevision = promptRevision();
    unorderedRevision.messages[0].content = [{ text: existingPromptText, type: "text" }];
    mockWorkbenchApi({ revisionsByPrompt: { prompt_1: [unorderedRevision] } });
    renderWorkbench();

    fireEvent.click(await screen.findByRole("button", { name: "Get Code" }));

    const dialog = screen.getByRole("dialog", { name: "Code for Claude API" });
    const codeText = dialog.querySelector("code")?.textContent ?? "";
    expect(codeText.indexOf('"type": "text"')).toBeGreaterThan(-1);
    expect(codeText.indexOf('"type": "text"')).toBeLessThan(codeText.indexOf('"text":'));
    expect(within(dialog).getByRole("button", { name: "Copy code" })).toBeTruthy();
    expect(dialog.textContent).toContain("client.messages.create");
    expect(dialog.textContent).toContain("import anthropic");
    expect(dialog.textContent).toContain('api_key="my_api_key"');
    expect(dialog.textContent).toContain('"type": "text"');
    expect(dialog.textContent).toContain("claude-opus-4-8");
    const languageCombobox = within(dialog).getByRole("combobox", { name: "Code language" });
    expect(languageCombobox.getAttribute("data-slot")).toBe("select-trigger");
    expect(languageCombobox.getAttribute("aria-expanded")).toBe("false");
    expect(languageCombobox.className.includes("bg-secondary")).toBe(false);
    expect(within(dialog).queryByRole("option", { name: "AWS Bedrock Python" })).toBeNull();
    expect(within(dialog).getByRole("button", { name: "View Docs" })).toBeTruthy();
    expect(
      within(dialog).getByRole("button", { name: "Close" }).classList.contains("workbench-code-dialog-close"),
    ).toBe(true);
    expect(dialog.querySelector("code.language-python")).toBeTruthy();
    expect(dialog.querySelector(".hljs-string")).toBeTruthy();
    expect(dialog.classList.contains("workbench-code-dialog")).toBe(true);
    expect(dialog.querySelector(".workbench-code-line-number")?.textContent).toBe("1");

    fireEvent.click(languageCombobox);
    await waitFor(() => expect(languageCombobox.getAttribute("aria-expanded")).toBe("true"));
    await waitFor(() => expect(screen.getByRole("listbox")).toBeTruthy());
    expect(screen.getByRole("option", { name: "Python" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("option", { name: "AWS Bedrock Python" })).toBeTruthy();
    expect(screen.getByRole("option", { name: "AWS Bedrock TypeScript" })).toBeTruthy();
    expect(screen.getByRole("option", { name: "Vertex AI Python" })).toBeTruthy();
    expect(screen.getByRole("option", { name: "Vertex AI TypeScript" })).toBeTruthy();
    expect(screen.queryByRole("option", { name: "JSON" })).toBeNull();

    selectOption("AWS Bedrock Python");
    await waitFor(() => expect(languageCombobox.textContent).toContain("AWS Bedrock Python"));
    expect(dialog.textContent).toContain("from anthropic import AnthropicBedrock");
    expect(dialog.textContent).toContain('model=""');
    expect(dialog.textContent).toContain("claude-on-amazon-bedrock");
  });

  test("opens generated code with the current draft when prompt changes are unsaved", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi({
      revisionsByPrompt: {
        prompt_1: [
          promptRevision(existingPromptText, "workbench-revision-latest"),
          promptRevision("Earlier saved prompt.", "workbench-revision-earlier"),
        ],
      },
    });
    renderWorkbench();

    await screen.findByRole("button", { name: "Get Code" });
    fireEvent.change(screen.getByLabelText("User prompt 1"), {
      target: { value: "Draft-only prompt before code." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Get Code" }));

    const dialog = await screen.findByRole("dialog", { name: "Code for Claude API" });
    expect(dialog.textContent).toContain("Draft-only prompt before code.");
    expect(screen.queryByLabelText("Version history")).toBeNull();
  });

  test("opens the generated code modal and clears the model drawer", async () => {
    resetTestDom("https://oma.duck.ai/workbench");
    mockWorkbenchApi();
    renderWorkbench();

    fireEvent.click(await screen.findByRole("button", { name: "Model settings" }));
    expect(await screen.findByRole("button", { name: "Maximum length of Claude’s responses" })).toBeTruthy();

    fireEvent.click(await screen.findByRole("button", { name: "Get Code" }));

    expect(await screen.findByRole("dialog", { name: "Code for Claude API" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Maximum length of Claude’s responses" })).toBeNull();
  });
});

function renderWorkbench({
  auth = authContextValue(),
  workspace = workspaceContextValue("default"),
}: {
  auth?: ReturnType<typeof authContextValue>;
  workspace?: ReturnType<typeof workspaceContextValue>;
} = {}) {
  return render(
    <AuthContext.Provider value={auth}>
      <WorkspaceContext.Provider value={workspace}>
        <WorkbenchPage />
      </WorkspaceContext.Provider>
    </AuthContext.Provider>,
  );
}

function defaultAuthAccount() {
  return {
    tagged_id: "user_default",
    uuid: "user_default",
    email_address: "admin@example.local",
    full_name: "Local Admin",
    display_name: "Local Admin",
  };
}

function authContextValue(account: any = defaultAuthAccount()) {
  return {
    account,
    status: "authenticated" as const,
    csrfToken: "csrf_test",
    refresh: async () => ({ account: null }),
    logout: async () => undefined,
  };
}

function workspaceContextValue(activeWorkspaceId: string) {
  return {
    orgUuid: "org_test",
    workspaces: [defaultWorkspace],
    activeWorkspace: defaultWorkspace,
    activeWorkspaceId,
    isLoading: false,
    error: null,
    selectWorkspace: () => undefined,
    createWorkspace: async () => defaultWorkspace,
    refreshWorkspaces: async () => undefined,
  };
}

type MockRequest = {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: Record<string, any> | undefined;
};

type WorkbenchPromptCreator = {
  tagged_id?: string;
  uuid?: string;
  full_name?: string;
  email_address?: string;
};

type MockPromptSummary = {
  id: string;
  name: string;
  workspace_id?: string;
  created_at?: string;
  updated_at?: string;
  creator?: WorkbenchPromptCreator;
};

function mockWorkbenchApi(
  options: {
    createPromptDelayMs?: number;
    failWorkspacePromptsOnce?: boolean;
    initialPromptName?: string;
    initialPromptText?: string;
    promptSummaries?: MockPromptSummary[];
    promptNames?: Record<string, string>;
    promptTexts?: Record<string, string>;
    promptExamples?: Array<Record<string, unknown>>;
    promptCreators?: Record<string, WorkbenchPromptCreator>;
    revisionIds?: Record<string, string>;
    revisionsByPrompt?: Record<string, ReturnType<typeof promptRevision>[]>;
    evaluationsByRevision?: Record<string, Array<Record<string, unknown>>>;
    initialTools?: Array<Record<string, unknown>>;
    uploadedFile?: { filename?: string; mime_type?: string };
    generatedPromptText?: string;
    generatedPromptDelayMs?: number;
    completionErrorMessage?: string;
    prepaidCreditsAmount?: number;
  } = {},
) {
  const requests: MockRequest[] = [];
  let createdPromptCount = 0;
  let workspacePromptsFailureCount = options.failWorkspacePromptsOnce ? 1 : 0;
  const createdPrompts: MockPromptSummary[] = [];
  const createdPromptRevisions = new Map<string, ReturnType<typeof promptRevision>>();
  const promptNames = new Map<string, string>();
  const storedEvaluations = new Map<string, Record<string, unknown>>();
  const getPromptRevisions = (promptId: string) =>
    createdPromptRevisions.has(promptId)
      ? [createdPromptRevisions.get(promptId)!]
      : (options.revisionsByPrompt?.[promptId] ?? [
          promptRevision(
            options.promptTexts?.[promptId] ?? options.initialPromptText ?? existingPromptText,
            options.revisionIds?.[promptId],
            options.initialTools,
          ),
        ]);
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.pathname + input.search : input.url;
    const method = init?.method ?? "GET";
    const headers = Object.fromEntries(new Headers(init?.headers).entries());
    const body = typeof init?.body === "string" ? JSON.parse(init.body) : undefined;
    requests.push({ url, method, headers, body });

    if (url === "/v1/files?beta=true" && method === "POST") {
      const uploadedFile = init?.body instanceof FormData ? init.body.get("file") : undefined;
      const uploadedFilename = uploadedFile instanceof File ? uploadedFile.name : undefined;
      return jsonResponse({
        id: "file_upload_test",
        type: "file",
        filename: uploadedFilename ?? options.uploadedFile?.filename ?? "brief.pdf",
        mime_type: options.uploadedFile?.mime_type ?? "application/pdf",
        size_bytes: 12,
        downloadable: false,
      });
    }

    if (url.endsWith("/models")) {
      return jsonResponse({
        default_prompt_settings: {
          model_name: "claude-opus-4-8",
          system_prompt: "",
          temperature: 1,
          max_tokens_to_sample: 20000,
        },
        models: [
          {
            model_name: "claude-opus-4-8",
            display_name: "Claude Opus Active",
            supports_thinking: true,
            supports_tool_use: true,
          },
          {
            model_name: "claude-sonnet-4-6",
            display_name: "Claude Sonnet Active",
            supports_thinking: true,
            supports_tool_use: true,
          },
        ],
      });
    }

    if (url.endsWith("/workspaces/default/prompts") && method === "GET") {
      if (workspacePromptsFailureCount > 0) {
        workspacePromptsFailureCount -= 1;
        return jsonResponse({ error: { message: "Prompt list unavailable." } }, 503);
      }
      const summaries = options.promptSummaries ?? defaultPromptSummaries();
      return jsonResponse(
        [...createdPrompts, ...summaries].map((item) => ({ ...item, name: promptNames.get(item.id) ?? item.name })),
      );
    }

    if (url.endsWith("/workbench/prompts") && method === "GET") {
      const summaries = options.promptSummaries ?? defaultPromptSummaries();
      return jsonResponse(
        [...createdPrompts, ...summaries].map((item) => ({ ...item, name: promptNames.get(item.id) ?? item.name })),
      );
    }

    if (url.endsWith("/workspaces/default/prompts") && method === "POST") {
      if (options.createPromptDelayMs) {
        await new Promise((resolve) => setTimeout(resolve, options.createPromptDelayMs));
      }
      createdPromptCount += 1;
      const promptId = `prompt_new_${createdPromptCount}`;
      const name = typeof body?.name === "string" ? body.name : "";
      const latestRevision = body?.latest_revision
        ? {
            ...promptRevision("", body.latest_revision.id ?? "workbench-revision-created"),
            ...body.latest_revision,
            is_latest: true,
          }
        : promptRevision("");
      createdPrompts.unshift({ id: promptId, name, workspace_id: "default", creator: defaultPromptCreator() });
      promptNames.set(promptId, name);
      createdPromptRevisions.set(promptId, latestRevision);
      return jsonResponse({
        ...promptDetail(promptId, "", name),
        latest_revision: latestRevision,
      });
    }

    if (/\/workbench\/prompts\/[^/?#]+\/kv_store\/get\/draft_revision$/.test(url)) {
      return jsonResponse({ success: false });
    }

    if (/\/workbench\/prompts\/[^/?#]+\/kv_store\/set\/draft_revision$/.test(url)) {
      return jsonResponse({ success: true, version: "saved_version" });
    }

    if (/\/workbench\/prompts\/[^/?#]+\/revisions$/.test(url) && method === "POST") {
      return jsonResponse({
        ...promptRevision(),
        ...body,
        id: "workbench-revision-saved",
        created_at: "2026-06-12T02:11:00.000000Z",
        is_latest: true,
      });
    }

    const revisionsMatch = /\/workbench\/prompts\/([^/?#]+)\/revisions(?:\?compact=true)?$/.exec(url);
    if (revisionsMatch && method === "GET") {
      const revisions = getPromptRevisions(decodeURIComponent(revisionsMatch[1]));
      if (url.includes("?compact=true")) {
        return jsonResponse(revisions.map(({ messages: _messages, ...revision }) => revision));
      }
      return jsonResponse(revisions);
    }

    const revisionDetailMatch = /\/workbench\/prompts\/([^/?#]+)\/revisions\/([^/?#]+)$/.exec(url);
    if (revisionDetailMatch && method === "GET") {
      const promptId = decodeURIComponent(revisionDetailMatch[1]);
      const revisionId = decodeURIComponent(revisionDetailMatch[2]);
      return jsonResponse(
        getPromptRevisions(promptId).find((revision) => revision.id === revisionId) ??
          promptRevision(existingPromptText, revisionId),
      );
    }

    const promptSharingMatch = /\/workbench\/prompts\/([^/?#]+)\/sharing$/.exec(url);
    if (promptSharingMatch && method === "POST") {
      const promptId = decodeURIComponent(promptSharingMatch[1]);
      return jsonResponse({
        ...promptDetail(
          promptId,
          options.promptTexts?.[promptId] ?? options.initialPromptText ?? existingPromptText,
          options.promptNames?.[promptId] ?? options.initialPromptName,
          options.revisionIds?.[promptId],
          options.promptCreators?.[promptId],
          options.promptExamples,
          options.initialTools,
        ),
        is_shared_with_workspace: true,
      });
    }

    const promptMatch = /\/workbench\/prompts\/([^/?#]+)$/.exec(url);
    if (promptMatch) {
      const promptId = decodeURIComponent(promptMatch[1]);
      if (method === "PUT") {
        if (typeof body?.name === "string") {
          promptNames.set(promptId, body.name);
        }
        return jsonResponse(
          promptDetail(
            promptId,
            options.promptTexts?.[promptId] ?? options.initialPromptText ?? existingPromptText,
            promptNames.get(promptId) ?? options.promptNames?.[promptId] ?? options.initialPromptName,
            options.revisionIds?.[promptId],
            options.promptCreators?.[promptId],
            options.promptExamples,
            options.initialTools,
          ),
        );
      }
      if (method === "DELETE") {
        return jsonResponse({ id: promptId, deleted: true });
      }
      return jsonResponse(
        promptDetail(
          promptId,
          options.promptTexts?.[promptId] ?? options.initialPromptText ?? existingPromptText,
          promptNames.get(promptId) ?? options.promptNames?.[promptId] ?? options.initialPromptName,
          options.revisionIds?.[promptId],
          options.promptCreators?.[promptId],
          options.promptExamples,
          options.initialTools,
        ),
      );
    }

    if (url.endsWith("/workbench/completions")) {
      if (options.completionErrorMessage) {
        return jsonResponse({ error: { message: options.completionErrorMessage } }, 500);
      }
      return sseResponse(
        [
          ["message_start", { type: "message_start", message: { id: "msg_test", role: "assistant", content: [] } }],
          [
            "content_block_delta",
            { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "Drafted response." } },
          ],
          ["message_stop", { type: "message_stop" }],
        ],
        0,
        init?.signal,
      );
    }

    if (/\/workbench\/revisions\/[^/?#]+\/evaluations\/create$/.test(url) && method === "POST") {
      const evaluation = {
        id: body?.id ?? "eval_created",
        revision_id: "workbench-revision-test",
        test_case_id: body?.test_case_id ?? body?.id ?? "eval_created",
        variable_values: body?.variable_values ?? {},
        golden_answer: body?.golden_answer ?? "",
        completion_text: body?.completion_text ?? "",
        rating: body?.rating ?? "",
      };
      storedEvaluations.set(String(evaluation.id), evaluation);
      return jsonResponse(evaluation);
    }

    const evaluationUpdateMatch =
      /\/workbench\/evaluations\/([^/?#]+)\/(update_variables|update_golden_answer|save_completion|update_rating|delete)$/.exec(
        url,
      );
    if (evaluationUpdateMatch && method === "POST") {
      const evaluationId = decodeURIComponent(evaluationUpdateMatch[1]);
      const action = evaluationUpdateMatch[2];
      const existing = storedEvaluations.get(evaluationId) ?? {
        id: evaluationId,
        revision_id: "workbench-revision-test",
        test_case_id: evaluationId,
        variable_values: {},
        golden_answer: "",
        completion_text: "",
        rating: "",
      };
      const next = { ...existing };
      if (action === "update_variables") {
        next.variable_values = body?.variable_values ?? {};
      } else if (action === "update_golden_answer") {
        next.golden_answer = body?.golden_answer ?? "";
      } else if (action === "save_completion") {
        next.completion_text = body?.completion_text ?? "";
      } else if (action === "update_rating") {
        next.rating = body?.rating ?? "";
      } else if (action === "delete") {
        storedEvaluations.delete(evaluationId);
        return jsonResponse(next);
      }
      storedEvaluations.set(evaluationId, next);
      return jsonResponse(next);
    }

    if (url.endsWith("/workbench/generate_title")) {
      return jsonResponse({ completion: "Cat haiku" });
    }

    if (url.endsWith("/workbench/metaprompt/generate_test_cases")) {
      const count = Math.max(1, Number(body?.num_testcases) || 1);
      return sseResponse(
        Array.from({ length: count }, (_, index) => [
          "test_case",
          { variable_values: { animal: `generated animal ${index + 1}` } },
        ]),
        0,
        init?.signal,
      );
    }

    if (url.endsWith("/workbench/evaluations/generate_test_case")) {
      return sseResponse(
        [
          [
            "content_block_delta",
            {
              type: "content_block_delta",
              index: 0,
              delta: {
                type: "text_delta",
                text: "<planning>Generated local Workbench test case.</planning>\n<animal>owl</animal>\nA quiet owl answers in moonlight.",
              },
            },
          ],
          ["message_stop", { type: "message_stop" }],
        ],
        0,
        init?.signal,
      );
    }

    if (url.endsWith("/workbench/generate_prompt")) {
      return sseResponse(
        [
          [
            "content_block_delta",
            {
              type: "content_block_delta",
              index: 0,
              delta: { type: "text_delta", text: options.generatedPromptText ?? "Improved prompt." },
            },
          ],
          ["message_stop", { type: "message_stop" }],
        ],
        options.generatedPromptDelayMs,
        init?.signal,
      );
    }

    if (url.endsWith("/prepaid/credits")) {
      return jsonResponse({ amount: options.prepaidCreditsAmount ?? 100, currency: "usd" });
    }

    const evaluationListMatch = /\/workbench\/revisions\/([^/?#]+)\/evaluations\/list$/.exec(url);
    if (evaluationListMatch) {
      return jsonResponse(options.evaluationsByRevision?.[decodeURIComponent(evaluationListMatch[1])] ?? []);
    }

    return jsonResponse({ error: { message: `Unhandled test route: ${method} ${url}` } }, 500);
  }) as typeof fetch;
  return { requests };
}

const existingPromptText = `You are Claude, an expert assistant.

Goal
Draft an email responding to a customer complaint email and offer a resolution`;

function defaultPromptCreator(): WorkbenchPromptCreator {
  return {
    tagged_id: "user_default",
    uuid: "user_default",
    full_name: "Local Admin",
    email_address: "admin@example.local",
  };
}

function defaultPromptSummaries(): MockPromptSummary[] {
  return [
    {
      id: "prompt_1",
      name: "Complaint response",
      workspace_id: "default",
      created_at: "2026-06-12T02:10:24.382428Z",
      updated_at: "2026-06-12T02:10:24.382428Z",
      creator: defaultPromptCreator(),
    },
  ];
}

function promptDetail(
  id = "prompt_1",
  text = existingPromptText,
  name?: string,
  revisionId?: string,
  creator: WorkbenchPromptCreator = defaultPromptCreator(),
  examples?: Array<Record<string, unknown>>,
  tools: Array<Record<string, unknown>> = [],
) {
  return {
    id,
    name: name ?? (text.trim() ? "Complaint response" : ""),
    workspace_id: "default",
    created_at: "2026-06-12T02:10:24.382428Z",
    updated_at: "2026-06-12T02:10:24.382428Z",
    is_shared_with_workspace: false,
    creator,
    latest_revision: promptRevision(text, revisionId, tools),
    kv_store: examples ? { examples } : {},
  };
}

function promptRevision(
  text = existingPromptText,
  id = "workbench-revision-test",
  tools: Array<Record<string, unknown>> = [],
) {
  return {
    id,
    created_at: "2026-06-12T02:10:24.382428Z",
    is_latest: true,
    model_name: "claude-opus-4-8",
    system_prompt: "",
    variables: [],
    max_tokens_to_sample: 20000,
    temperature: 1,
    thinking: { type: "enabled", effort: "high", budget_tokens: 16000 },
    show_raw_thinking: false,
    skip_system_modification: false,
    tools,
    messages: [
      {
        role: "human",
        content: [{ type: "text", text }],
      },
      {
        role: "assistant",
        content: [{ type: "text", text: "" }],
      },
    ],
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function sseResponse(events: [string, Record<string, unknown>][], delayMs = 0, signal?: AbortSignal) {
  const body = events.map(([event, data]) => `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`).join("");
  return new Response(
    new ReadableStream({
      start(controller) {
        let finished = false;
        let timeoutId: ReturnType<typeof setTimeout> | null = null;
        const cleanup = () => signal?.removeEventListener("abort", abort);
        const write = () => {
          if (finished) {
            return;
          }
          finished = true;
          cleanup();
          controller.enqueue(new TextEncoder().encode(body));
          controller.close();
        };
        const abort = () => {
          if (finished) {
            return;
          }
          finished = true;
          if (timeoutId !== null) {
            clearTimeout(timeoutId);
          }
          cleanup();
          controller.error(new DOMException("The operation was aborted.", "AbortError"));
        };
        if (signal?.aborted) {
          abort();
          return;
        }
        signal?.addEventListener("abort", abort);
        if (delayMs > 0) {
          timeoutId = setTimeout(write, delayMs);
          return;
        }
        write();
      },
    }),
    { headers: { "Content-Type": "text/event-stream" } },
  );
}
