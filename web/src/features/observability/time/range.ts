export type TimeZoneMode = 'browser' | 'utc';

export interface RawTimeRange {
  from: string;
  to: string;
}

export interface ResolvedRange {
  start: number;
  end: number;
}

export type RangeProblem = 'invalid' | 'order' | 'tooShort' | 'tooLong';

type Unit = 's' | 'm' | 'h' | 'd' | 'w' | 'M' | 'y';

interface Parts {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  second: number;
  millisecond: number;
}

export const DEFAULT_RANGE: RawTimeRange = { from: 'now-7d', to: 'now' };
export const MIN_RANGE_MS = 60_000;
export const MAX_RANGE_MS = 366 * 86_400_000;

const FIXED_MS: Partial<Record<Unit, number>> = { s: 1_000, m: 60_000, h: 3_600_000, d: 86_400_000, w: 604_800_000 };
const RELATIVE_PATTERN = /^now((?:[+-]\d+[smhdwMy])*)(?:\/([smhdwMy]))?$/;
const OFFSET_PATTERN = /([+-])(\d+)([smhdwMy])/g;
const ABSOLUTE_PATTERN = /^(\d{4})-(\d{2})-(\d{2})(?:[ T](\d{2}):(\d{2})(?::(\d{2}))?)?$/;
const EPOCH_PATTERN = /^\d{13}$/;
const MAX_DATE_MS = 8.64e15;

function toParts(ms: number, tz: TimeZoneMode): Parts {
  const date = new Date(ms);
  if (tz === 'utc')
    return {
      year: date.getUTCFullYear(),
      month: date.getUTCMonth(),
      day: date.getUTCDate(),
      hour: date.getUTCHours(),
      minute: date.getUTCMinutes(),
      second: date.getUTCSeconds(),
      millisecond: date.getUTCMilliseconds(),
    };
  return {
    year: date.getFullYear(),
    month: date.getMonth(),
    day: date.getDate(),
    hour: date.getHours(),
    minute: date.getMinutes(),
    second: date.getSeconds(),
    millisecond: date.getMilliseconds(),
  };
}

function fromParts(parts: Parts, tz: TimeZoneMode) {
  const fields = [
    parts.year,
    parts.month,
    parts.day,
    parts.hour,
    parts.minute,
    parts.second,
    parts.millisecond,
  ] as const;
  return tz === 'utc' ? Date.UTC(...fields) : new Date(...fields).getTime();
}

function addUnit(ms: number, amount: number, unit: Unit, tz: TimeZoneMode) {
  const fixed = FIXED_MS[unit];
  if (fixed && unit !== 'd' && unit !== 'w') return ms + amount * fixed;
  const parts = toParts(ms, tz);
  if (unit === 'd') parts.day += amount;
  if (unit === 'w') parts.day += amount * 7;
  if (unit === 'M' || unit === 'y') {
    const month = parts.year * 12 + parts.month + (unit === 'y' ? amount * 12 : amount);
    parts.year = Math.floor(month / 12);
    parts.month = month - parts.year * 12;
    parts.day = Math.min(parts.day, new Date(Date.UTC(parts.year, parts.month + 1, 0)).getUTCDate());
  }
  return fromParts(parts, tz);
}

const UNIT_ORDER: Unit[] = ['y', 'M', 'w', 'd', 'h', 'm', 's'];

function startOf(ms: number, unit: Unit, tz: TimeZoneMode) {
  const parts = toParts(ms, tz);
  const depth = UNIT_ORDER.indexOf(unit);
  parts.millisecond = 0;
  if (depth < 6) parts.second = 0;
  if (depth < 5) parts.minute = 0;
  if (depth < 4) parts.hour = 0;
  if (unit === 'w') parts.day -= (new Date(Date.UTC(parts.year, parts.month, parts.day)).getUTCDay() + 6) % 7;
  if (depth < 2) parts.day = 1;
  if (depth < 1) parts.month = 0;
  return fromParts(parts, tz);
}

function parseRelative(text: string, now: number, tz: TimeZoneMode, roundUp: boolean) {
  const match = RELATIVE_PATTERN.exec(text);
  if (!match) return null;
  let value = now;
  for (const [, sign, amount, unit] of match[1].matchAll(OFFSET_PATTERN))
    value = addUnit(value, (sign === '-' ? -1 : 1) * Number(amount), unit as Unit, tz);
  const round = match[2] as Unit | undefined;
  if (!round) return value;
  const start = startOf(value, round, tz);
  return roundUp ? addUnit(start, 1, round, tz) - 1 : start;
}

const PART_FIELDS = ['year', 'month', 'day', 'hour', 'minute', 'second'] as const;

function parseAbsolute(text: string, tz: TimeZoneMode) {
  if (EPOCH_PATTERN.test(text)) return Number(text);
  const match = ABSOLUTE_PATTERN.exec(text);
  if (!match) return null;
  const [year, month, day, hour, minute, second] = match.slice(1).map((part) => Number(part ?? 0));
  const parts = { year, month: month - 1, day, hour, minute, second, millisecond: 0 };
  const value = fromParts(parts, tz);
  const check = toParts(value, tz);
  return PART_FIELDS.every((field) => check[field] === parts[field]) ? value : null;
}

export function parseTime(text: string, now: number, tz: TimeZoneMode, roundUp = false) {
  const value = text.trim();
  const parsed = value.startsWith('now') ? parseRelative(value, now, tz, roundUp) : parseAbsolute(value, tz);
  return parsed != null && Math.abs(parsed) <= MAX_DATE_MS ? parsed : null;
}

function spanProblem(start: number | null, end: number | null): RangeProblem | null {
  if (start == null || end == null) return 'invalid';
  if (end <= start) return 'order';
  if (end - start < MIN_RANGE_MS) return 'tooShort';
  return end - start > MAX_RANGE_MS ? 'tooLong' : null;
}

export function rangeProblem(raw: RawTimeRange, now: number, tz: TimeZoneMode): RangeProblem | null {
  return spanProblem(parseTime(raw.from, now, tz), parseTime(raw.to, now, tz, true));
}

export function resolveRange(raw: RawTimeRange, now: number, tz: TimeZoneMode): ResolvedRange | null {
  const start = parseTime(raw.from, now, tz);
  const end = parseTime(raw.to, now, tz, true);
  return start != null && end != null && !spanProblem(start, end) ? { start, end } : null;
}

export function isRelative(raw: RawTimeRange) {
  return raw.from.startsWith('now') || raw.to.startsWith('now');
}

export function absoluteRange(start: number, end: number): RawTimeRange {
  return { from: String(Math.round(start)), to: String(Math.round(end)) };
}

export function shiftRange(range: ResolvedRange, direction: -1 | 1, now: number): RawTimeRange {
  const span = range.end - range.start;
  const end = Math.min(range.end + (direction * span) / 2, Math.max(now, range.end));
  return absoluteRange(end - span, end);
}

export function zoomOutRange(raw: RawTimeRange, range: ResolvedRange): RawTimeRange {
  const relative = /^now-(\d+)([smhdwMy])$/.exec(raw.from);
  const span = Math.min((range.end - range.start) * 2, MAX_RANGE_MS);
  if (relative && raw.to === 'now' && span < MAX_RANGE_MS)
    return { from: `now-${Number(relative[1]) * 2}${relative[2]}`, to: 'now' };
  const center = (range.start + range.end) / 2;
  return absoluteRange(center - span / 2, center + span / 2);
}

export function timeZoneOption(tz: TimeZoneMode) {
  return tz === 'utc' ? 'UTC' : undefined;
}

function pad(value: number, length = 2) {
  return String(value).padStart(length, '0');
}

export function formatAbsolute(ms: number, tz: TimeZoneMode) {
  const parts = toParts(ms, tz);
  return `${parts.year}-${pad(parts.month + 1)}-${pad(parts.day)} ${pad(parts.hour)}:${pad(parts.minute)}:${pad(parts.second)}`;
}

export function inputText(text: string, tz: TimeZoneMode) {
  return EPOCH_PATTERN.test(text) ? formatAbsolute(Number(text), tz) : text;
}

export function storedText(text: string, now: number, tz: TimeZoneMode, roundUp: boolean) {
  const value = text.trim();
  if (value.startsWith('now')) return value;
  const parsed = parseTime(value, now, tz, roundUp);
  return parsed == null ? value : String(parsed);
}

export function utcOffsetLabel(now: number, tz: TimeZoneMode) {
  const offset = tz === 'utc' ? 0 : -new Date(now).getTimezoneOffset();
  const sign = offset < 0 ? '-' : '+';
  return `UTC${sign}${pad(Math.floor(Math.abs(offset) / 60))}:${pad(Math.abs(offset) % 60)}`;
}

export const REFRESH_INTERVALS = ['10s', '30s', '1m', '5m', '15m', '30m', '1h'] as const;

export function refreshMs(refresh: string) {
  const match = /^(\d+)([smh])$/.exec(refresh);
  return match ? Number(match[1]) * FIXED_MS[match[2] as Unit]! : null;
}
