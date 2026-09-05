import { createIntl } from 'react-intl';
import zhMessages from '@/shared/i18n/messages/zh-CN.json';
import enMessages from '@/shared/i18n/messages/en.json';
import { expect, test } from 'bun:test';
import { sessionEventAppearsInTranscript, sessionEventPreview } from './sessionTraceModel';

test('Git preparation is visible in the conversation while ordinary system messages stay hidden', () => {
  const options = { platformTranscriptFiltering: true };
  expect(
    sessionEventAppearsInTranscript(
      { type: 'system.message', subtype: 'git_repository', resource_status: 'started' },
      options,
    ),
  ).toBe(true);
  expect(sessionEventAppearsInTranscript({ type: 'system.message', subtype: 'init' }, options)).toBe(false);
});

test('Git preparation preview shows the preparation result directly', () => {
  expect(
    sessionEventPreview(
      { type: 'system.message', subtype: 'git_repository' },
      'Git repository ready: https://github.com/octocat/Hello-World → /workspace/public-repo',
      'system',
    ),
  ).toBe('Git repository ready: https://github.com/octocat/Hello-World → /workspace/public-repo');
});

test('Git preparation shows English duration and safe failure reasons in both locales, including old events', () => {
  const intl = createIntl({ locale: 'zh-CN', messages: zhMessages });
  const msg = (id: string, defaultMessage: string, values?: Record<string, string | number>) =>
    intl.formatMessage({ id, defaultMessage }, values);
  const event = {
    type: 'system.message',
    subtype: 'git_repository',
    url: 'https://github.com/octocat/Hello-World',
    duration_ms: 1234,
  };
  expect(sessionEventPreview({ ...event, resource_status: 'ready' }, '', 'system', msg)).toBe(
    'Git repository ready: https://github.com/octocat/Hello-World (Duration: 1.2 s)',
  );
  expect(
    sessionEventPreview({ ...event, resource_status: 'failed', failure_reason: 'access_denied' }, '', 'system', msg),
  ).toContain('Repository not found or access denied');
  expect(
    sessionEventPreview({ ...event, duration_ms: undefined, resource_status: 'failed' }, '', 'system', msg),
  ).toContain('No failure details were recorded');
  expect(
    sessionEventPreview({ ...event, duration_ms: undefined, resource_status: 'ready' }, '', 'system', msg),
  ).toContain('Duration: Not recorded');
  expect(
    sessionEventPreview({ ...event, resource_status: 'failed', failure_reason: 'secret-value' }, '', 'system', msg),
  ).not.toContain('secret-value');
  expect(sessionEventPreview({ ...event, resource_status: 'unknown' }, 'original event', 'system', msg)).toBe(
    'original event',
  );
  const english = createIntl({ locale: 'en', messages: enMessages });
  expect(
    sessionEventPreview({ ...event, resource_status: 'ready' }, '', 'system', (id, defaultMessage, values) =>
      english.formatMessage({ id, defaultMessage }, values),
    ),
  ).toBe('Git repository ready: https://github.com/octocat/Hello-World (Duration: 1.2 s)');
});
