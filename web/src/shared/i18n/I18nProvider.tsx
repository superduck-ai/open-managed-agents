import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import {
  IntlProvider,
  ReactIntlErrorCode,
  createIntl,
  createIntlCache,
  type IntlConfig,
  type MessageDescriptor
} from 'react-intl';
import {
  I18nContext,
  defaultLocale,
  supportedLocales,
  type I18nContextValue,
  type Locale,
  type MessageValues
} from './context';
import enMessages from './messages/en.json';
import zhCnMessages from './messages/zh-CN.json';

const storageKey = 'oma.locale';
const intlCache = createIntlCache();

const messageCatalogs: Record<Locale, Record<string, string>> = {
  en: enMessages,
  'zh-CN': zhCnMessages
};

export function initializeLocale() {
  if (typeof window === 'undefined') {
    return;
  }
  applyLocale(readLocale());
}

export function I18nProvider({ children, initialLocale }: { children: ReactNode; initialLocale?: Locale }) {
  const [locale, setLocaleState] = useState<Locale>(() => initialLocale ?? readLocale());
  const messages = messageCatalogs[locale];
  const intl = useMemo(
    () =>
      createIntl(
        {
          locale,
          defaultLocale,
          messages,
          onError: handleIntlError
        },
        intlCache
      ),
    [locale, messages]
  );

  useEffect(() => {
    applyLocale(locale);
    try {
      window.localStorage.setItem(storageKey, locale);
    } catch {
      // Locale still applies for the current session when storage is unavailable.
    }
  }, [locale]);

  const setLocale = useCallback((nextLocale: Locale) => {
    setLocaleState(nextLocale);
  }, []);

  const msg = useCallback(
    (id: string, defaultMessage: string, values?: MessageValues) =>
      intl.formatMessage({ id, defaultMessage } as MessageDescriptor, values),
    [intl]
  );

  const value = useMemo<I18nContextValue>(
    () => ({
      locale,
      setLocale,
      msg
    }),
    [locale, msg, setLocale]
  );

  return (
    <IntlProvider locale={locale} defaultLocale={defaultLocale} messages={messages} onError={handleIntlError}>
      <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
    </IntlProvider>
  );
}

export function readLocale(): Locale {
  if (typeof window === 'undefined') {
    return defaultLocale;
  }

  try {
    const stored = normalizeLocale(window.localStorage.getItem(storageKey));
    if (stored) {
      return stored;
    }
  } catch {
    return defaultLocale;
  }

  const browserLocales = window.navigator.languages?.length
    ? window.navigator.languages
    : [window.navigator.language].filter(Boolean);
  for (const browserLocale of browserLocales) {
    const normalized = normalizeLocale(browserLocale);
    if (normalized) {
      return normalized;
    }
  }

  return defaultLocale;
}

export function normalizeLocale(value: string | null | undefined): Locale | null {
  if (!value) {
    return null;
  }
  const normalized = value.replace('_', '-').toLowerCase();
  if (normalized === 'en' || normalized.startsWith('en-')) {
    return 'en';
  }
  if (normalized === 'zh' || normalized.startsWith('zh-')) {
    return 'zh-CN';
  }
  return supportedLocales.find((locale) => locale.toLowerCase() === normalized) ?? null;
}

function applyLocale(locale: Locale) {
  document.documentElement.lang = locale;
  document.documentElement.dir = 'ltr';
  document.documentElement.dataset.locale = locale;
}

function handleIntlError(error: Parameters<NonNullable<IntlConfig['onError']>>[0]) {
  if (error.code === ReactIntlErrorCode.MISSING_TRANSLATION) {
    return;
  }
  console.error(error);
}
