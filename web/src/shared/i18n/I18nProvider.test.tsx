import { afterEach, describe, expect, test } from "bun:test";
import { resetTestDom } from "../../test/setup";
import { I18nProvider, normalizeLocale, readLocale, useI18n, useLocale } from "./index";
import enMessages from "./messages/en.json";
import zhCnMessages from "./messages/zh-CN.json";

const testingLibrary = await import("@testing-library/react");
const { cleanup, fireEvent, render, screen } = testingLibrary;

const navigatorLanguagesDescriptor = Object.getOwnPropertyDescriptor(window.navigator, "languages");

afterEach(() => {
  cleanup();
  window.localStorage.clear();
  if (navigatorLanguagesDescriptor) {
    Object.defineProperty(window.navigator, "languages", navigatorLanguagesDescriptor);
  }
});

describe("i18n locale handling", () => {
  test("normalizes supported browser locale variants", () => {
    expect(normalizeLocale("en-US")).toBe("en");
    expect(normalizeLocale("zh-Hans-CN")).toBe("zh-CN");
    expect(normalizeLocale("zh_TW")).toBe("zh-CN");
    expect(normalizeLocale("fr-FR")).toBeNull();
  });

  test("reads localStorage first and falls back to browser Chinese or English", () => {
    resetTestDom("https://oma.duck.ai/dashboard");
    Object.defineProperty(window.navigator, "languages", { value: ["zh-Hans-CN", "en-US"], configurable: true });

    expect(readLocale()).toBe("zh-CN");

    window.localStorage.setItem("oma.locale", "en");
    expect(readLocale()).toBe("en");

    window.localStorage.setItem("oma.locale", "not-a-locale");
    Object.defineProperty(window.navigator, "languages", { value: ["fr-FR"], configurable: true });
    expect(readLocale()).toBe("en");
  });

  test("updates rendered messages, html lang, and persisted locale", () => {
    resetTestDom("https://oma.duck.ai/dashboard");

    render(
      <I18nProvider initialLocale="en">
        <LocaleProbe />
      </I18nProvider>,
    );

    expect(document.documentElement.lang).toBe("en");
    expect(screen.getByText("Dashboard")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Switch to zh-CN" }));

    expect(document.documentElement.lang).toBe("zh-CN");
    expect(document.documentElement.dataset.locale).toBe("zh-CN");
    expect(window.localStorage.getItem("oma.locale")).toBe("zh-CN");
    expect(screen.getByText("仪表盘")).toBeTruthy();
  });
});

describe("i18n catalogs", () => {
  test("keeps semantic keys aligned across locales", () => {
    const enKeys = Object.keys(enMessages).sort();
    const zhKeys = Object.keys(zhCnMessages).sort();

    expect(zhKeys).toEqual(enKeys);
    for (const key of enKeys) {
      expect(key.includes(".")).toBe(true);
      expect(key).not.toMatch(/^[a-f0-9]{16,}$/i);
      expect((enMessages as Record<string, string>)[key]?.trim()).not.toBe("");
      expect((zhCnMessages as Record<string, string>)[key]?.trim()).not.toBe("");
    }
  });

  test("keeps ICU parameter names consistent across translations", () => {
    for (const [key, defaultMessage] of Object.entries(enMessages as Record<string, string>)) {
      expect(extractIcuParams((zhCnMessages as Record<string, string>)[key] ?? "")).toEqual(
        extractIcuParams(defaultMessage),
      );
    }
  });
});

function LocaleProbe() {
  const { msg } = useI18n();
  const { setLocale } = useLocale();

  return (
    <div>
      <p>{msg("nav.dashboard", "Dashboard")}</p>
      <button type="button" onClick={() => setLocale("zh-CN")}>
        Switch to zh-CN
      </button>
    </div>
  );
}

function extractIcuParams(message: string) {
  return Array.from(message.matchAll(/\{([a-zA-Z][\w]*)[,}]/g), (match) => match[1]).sort();
}
