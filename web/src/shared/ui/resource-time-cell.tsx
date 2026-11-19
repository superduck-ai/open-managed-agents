import { useI18n } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';

export function formatFullDateTime(value: string | number | Date | null | undefined, locale: string): string {
  if (value === null || value === undefined || value === '') {
    return '';
  }
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return typeof value === 'string' ? value : '';
  }
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date);
}

export function formatRelativeDateTime(
  value: string | number | Date | null | undefined,
  locale: string,
  justNowLabel: string,
): string {
  if (value === null || value === undefined || value === '') {
    return '—';
  }
  const date = value instanceof Date ? value : new Date(value);
  const timestamp = date.getTime();
  if (Number.isNaN(timestamp)) {
    return typeof value === 'string' ? value : '—';
  }
  const diffSeconds = Math.round((timestamp - Date.now()) / 1000);
  const absSeconds = Math.abs(diffSeconds);
  if (absSeconds < 45) {
    return justNowLabel;
  }
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' });
  const absMinutes = Math.round(absSeconds / 60);
  if (absMinutes < 60) {
    return formatter.format(Math.round(diffSeconds / 60), 'minute');
  }
  const absHours = Math.round(absMinutes / 60);
  if (absHours < 24) {
    return formatter.format(Math.round(diffSeconds / 3600), 'hour');
  }
  const absDays = Math.round(absHours / 24);
  if (absDays < 30) {
    return formatter.format(Math.round(diffSeconds / 86400), 'day');
  }
  const absMonths = Math.round(absDays / 30);
  if (absMonths < 12) {
    return formatter.format(Math.round(diffSeconds / 2592000), 'month');
  }
  return formatter.format(Math.round(diffSeconds / 31536000), 'year');
}

export function RelativeTimeText({
  value,
  emptyLabel = '—',
  className,
}: {
  value: string | number | Date | null | undefined;
  emptyLabel?: string;
  className?: string;
}) {
  const { locale, msg } = useI18n();
  if (value === null || value === undefined || value === '') {
    return <span className={cn('text-muted-foreground', className)}>{emptyLabel}</span>;
  }
  const date = value instanceof Date ? value : new Date(value);
  const isValid = !Number.isNaN(date.getTime());
  const full = isValid ? formatFullDateTime(date, locale) : String(value);
  const relative = isValid ? formatRelativeDateTime(date, locale, msg('common.justNow', 'just now')) : String(value);
  return (
    <time
      dateTime={isValid ? date.toISOString() : undefined}
      title={full || undefined}
      className={cn('whitespace-nowrap tabular-nums text-muted-foreground', className)}
    >
      {relative}
    </time>
  );
}
