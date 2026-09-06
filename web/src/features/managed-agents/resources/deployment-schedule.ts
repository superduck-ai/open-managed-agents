import { CronExpressionParser } from 'cron-parser';
import { DateTime } from 'luxon';

export const scheduleFrequencies = ['minute', 'hour', 'daily', 'weekdays', 'weekly', 'custom'] as const;
export type ScheduleFrequency = (typeof scheduleFrequencies)[number];
export type ScheduleControls = { frequency: ScheduleFrequency; time: string; day: string };

export function scheduleToCron({ frequency, time, day }: ScheduleControls): string {
  if (frequency === 'minute') return '* * * * *';
  const [hour, minute] = time.split(':').map(Number);
  if (!/^\d{2}:\d{2}$/.test(time) || hour > 23 || minute > 59) return '';
  switch (frequency) {
    case 'hour':
      return `${minute} * * * *`;
    case 'daily':
      return `${minute} ${hour} * * *`;
    case 'weekdays':
      return `${minute} ${hour} * * 1-5`;
    case 'weekly':
      return `${minute} ${hour} * * ${day}`;
    case 'custom':
      return '';
  }
}

export function cronToSchedule(expression: string): ScheduleControls {
  const fallback: ScheduleControls = { frequency: 'custom', time: '09:00', day: '1' };
  const fields = expression.trim().split(/\s+/);
  if (fields.length !== 5) return fallback;
  const [minute, hour, date, month, day] = fields;
  if (date !== '*' || month !== '*') return fallback;
  if (expression.trim() === '* * * * *') return { ...fallback, frequency: 'minute' };
  if (!/^\d{1,2}$/.test(minute) || Number(minute) > 59) return fallback;
  const time = `${hour === '*' ? '09' : hour.padStart(2, '0')}:${minute.padStart(2, '0')}`;
  if (hour === '*' && day === '*') return { ...fallback, frequency: 'hour', time };
  if (!/^\d{1,2}$/.test(hour) || Number(hour) > 23) return fallback;
  if (day === '*') return { ...fallback, frequency: 'daily', time };
  if (day === '1-5') return { ...fallback, frequency: 'weekdays', time };
  if (/^[0-7]$/.test(day)) return { frequency: 'weekly', time, day: String(Number(day) % 7) };
  return fallback;
}

export type SchedulePreview = { runs: Date[]; error: 'cron' | 'timezone' | null };

export function previewSchedule(expression: string, timezone: string, now = new Date()): SchedulePreview {
  try {
    if (!timezone || timezone === 'Local') throw new Error('Invalid timezone');
    new Intl.DateTimeFormat('en', { timeZone: timezone }).format(now);
  } catch {
    return { runs: [], error: 'timezone' };
  }
  try {
    // Match the API's five-field POSIX contract; cron-parser also accepts extensions the API rejects.
    if (expression.trim().split(/\s+/).length !== 5 || /[LW#?@H]/.test(expression)) throw new Error('Invalid cron');
    return { runs: nextScheduleRuns(expression, timezone, now), error: null };
  } catch {
    return { runs: [], error: 'cron' };
  }
}

// Iterate calendar occurrences in UTC, then resolve the wall clock in the selected zone.
// This matches robfig: skip nonexistent times and include both offsets of a repeated time.
function nextScheduleRuns(expression: string, timezone: string, now: Date): Date[] {
  const localNow = DateTime.fromJSDate(now, { zone: timezone });
  const offsets = [localNow.minus({ days: 1 }).offset, localNow.offset, localNow.plus({ days: 1 }).offset];
  const overlap = Math.max(...offsets) - Math.min(...offsets);
  const start = localNow.setZone('UTC', { keepLocalTime: true }).minus({ minutes: overlap });
  const interval = CronExpressionParser.parse(expression, { tz: 'UTC', currentDate: start.toJSDate() });
  const runs: number[] = [];
  for (;;) {
    const wall = DateTime.fromJSDate(interval.next().toDate(), { zone: 'UTC' });
    const resolved = wall.setZone(timezone, { keepLocalTime: true });
    // Luxon moves nonexistent wall times forward. They are not runs under our API contract.
    if (resolved.hour !== wall.hour || resolved.day !== wall.day || resolved.minute !== wall.minute) continue;
    const candidates = resolved
      .getPossibleOffsets()
      .map((date) => date.toMillis())
      .sort((a, b) => a - b);
    if (runs.length >= 5 && candidates[0] > runs[4]) return runs.slice(0, 5).map((time) => new Date(time));
    runs.push(...candidates.filter((time) => time > now.getTime()));
    runs.sort((a, b) => a - b);
  }
}
