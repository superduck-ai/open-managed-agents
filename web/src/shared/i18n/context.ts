import { createContext, useContext } from 'react';
import { createIntl, createIntlCache, type MessageDescriptor, type PrimitiveType } from 'react-intl';
import enMessages from './messages/en.json';

export const supportedLocales = ['en', 'zh-CN'] as const;
export type Locale = (typeof supportedLocales)[number];
export type MessageValues = Record<string, PrimitiveType>;

export const defaultLocale: Locale = 'en';
export const localeLabels: Record<Locale, string> = {
  en: 'English',
  'zh-CN': '简体中文',
};

export type I18nContextValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  msg: (id: string, defaultMessage: string, values?: MessageValues) => string;
};

const fallbackIntl = createIntl(
  {
    locale: defaultLocale,
    defaultLocale,
    messages: enMessages,
  },
  createIntlCache(),
);

export const I18nContext = createContext<I18nContextValue>({
  locale: defaultLocale,
  setLocale: () => undefined,
  msg: (id, defaultMessage, values) => fallbackIntl.formatMessage({ id, defaultMessage } as MessageDescriptor, values),
});

export function useI18n() {
  return useContext(I18nContext);
}

export function useLocale() {
  const { locale, setLocale } = useI18n();
  return {
    locale,
    setLocale,
    supportedLocales,
  };
}
