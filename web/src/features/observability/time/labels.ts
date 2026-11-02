import { useI18n } from '../../../shared/i18n';
import { findQuickRange, type QuickRange } from './quickRanges';
import { formatAbsolute, parseTime, type RawTimeRange, type RangeProblem, type TimeZoneMode } from './range';

type Msg = ReturnType<typeof useI18n>['msg'];

export function quickRangeText(range: QuickRange, msg: Msg) {
  return msg(`observability.time.quick.${range.key}`, range.text);
}

const LAST_PATTERN = /^now-(\d+)([smhdwMy])$/;

const LAST_TEXT: Record<string, [string, string]> = {
  s: ['seconds', 'Last {count, plural, one {# second} other {# seconds}}'],
  m: ['minutes', 'Last {count, plural, one {# minute} other {# minutes}}'],
  h: ['hours', 'Last {count, plural, one {# hour} other {# hours}}'],
  d: ['days', 'Last {count, plural, one {# day} other {# days}}'],
  w: ['weeks', 'Last {count, plural, one {# week} other {# weeks}}'],
  M: ['months', 'Last {count, plural, one {# month} other {# months}}'],
  y: ['years', 'Last {count, plural, one {# year} other {# years}}'],
};

export function rangeText(raw: RawTimeRange, tz: TimeZoneMode, now: number, msg: Msg) {
  const quick = findQuickRange(raw);
  if (quick) return quickRangeText(quick, msg);
  const from = parseTime(raw.from, now, tz);
  const to = parseTime(raw.to, now, tz, true);
  if (from == null || to == null) return msg('observability.time.invalidRange', 'Invalid time range');
  const last = raw.to === 'now' ? LAST_PATTERN.exec(raw.from) : null;
  if (last) {
    const [unit, text] = LAST_TEXT[last[2]];
    return msg(`observability.time.last.${unit}`, text, { count: Number(last[1]) });
  }
  return msg('observability.time.rangeText', '{from} to {to}', {
    from: formatAbsolute(from, tz),
    to: formatAbsolute(to, tz),
  });
}

const PROBLEM_TEXT: Record<RangeProblem, string> = {
  invalid: 'Use a time such as now-6h or 2026-01-31 08:00:00.',
  order: '"From" must be before "To".',
  tooShort: 'The range must be at least one minute.',
  tooLong: 'The range can be at most one year.',
};

export function problemText(problem: RangeProblem, msg: Msg) {
  return msg(`observability.time.problem.${problem}`, PROBLEM_TEXT[problem]);
}
