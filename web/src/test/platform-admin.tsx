import './setup';
import type { ReactNode } from 'react';
const { render } = await import('@testing-library/react');
import { I18nProvider } from '../shared/i18n';
import type { Locale } from '../shared/i18n';

export function renderPlatformAdmin(ui: ReactNode, locale: Locale = 'zh-CN') {
  return render(ui, {
    wrapper: ({ children }) => <I18nProvider initialLocale={locale}>{children}</I18nProvider>,
  });
}
