import { afterEach, describe, expect, test } from 'bun:test';
import { I18nProvider } from '../../shared/i18n';
import { resetTestDom } from '../../test/setup';
import { CachingPage, LogsPage, RateLimitsPage } from './AnalyticsPages';

const { cleanup, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

const pages = [
  {
    Page: CachingPage,
    title: 'Caching',
    chineseTitle: '缓存',
    notice: 'Caching data is not connected.',
    chineseNotice: '缓存统计尚未接入。',
  },
  {
    Page: RateLimitsPage,
    title: 'Rate limits',
    chineseTitle: '速率限制',
    notice: 'Rate limit data is not connected.',
    chineseNotice: '速率限制数据尚未接入。',
  },
  {
    Page: LogsPage,
    title: 'Logs',
    chineseTitle: '日志',
    notice: 'Request logs are not connected.',
    chineseNotice: '请求日志尚未接入。',
  },
];

describe('Analytics pages', () => {
  for (const { Page, title, chineseTitle, notice, chineseNotice } of pages) {
    test.each(['en', 'zh-CN'] as const)(
      `${title} distinguishes unavailable data from empty results in %s`,
      (locale) => {
        resetTestDom('https://oma.duck.ai/usage');
        render(
          <I18nProvider initialLocale={locale}>
            <Page />
          </I18nProvider>,
        );

        expect(screen.getByRole('heading', { level: 1, name: locale === 'en' ? title : chineseTitle })).toBeTruthy();
        expect(screen.getByText(locale === 'en' ? notice : chineseNotice)).toBeTruthy();
        expect(screen.queryByText('0')).toBeNull();
        expect(screen.queryByText('$0.00')).toBeNull();
        expect(screen.queryByText('Sonnet 4.6')).toBeNull();
        expect(screen.queryByText('June 2026')).toBeNull();
        expect(screen.queryByText('Updated just now.')).toBeNull();
        expect(screen.queryByRole('combobox')).toBeNull();
        expect(screen.queryByRole('button', { name: /Refresh|刷新/ })).toBeNull();
      },
    );
  }

  test('keeps caching documentation available without claiming caching is unused', () => {
    resetTestDom('https://oma.duck.ai/usage/cache');
    render(
      <I18nProvider initialLocale="en">
        <CachingPage />
      </I18nProvider>,
    );

    expect(screen.getByRole('link', { name: /Learn more/ }).getAttribute('href')).toBe(
      'https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching',
    );
    expect(screen.queryByText("You're not using prompt caching")).toBeNull();
  });
});
