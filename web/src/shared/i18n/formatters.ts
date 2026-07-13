import { useMemo } from "react";
import { useLocale } from "./context";

export function useFormatters() {
  const { locale } = useLocale();

  return useMemo(
    () => ({
      date(value: string | number | Date, options?: Intl.DateTimeFormatOptions) {
        return new Intl.DateTimeFormat(locale, options).format(new Date(value));
      },
      time(value: string | number | Date, options?: Intl.DateTimeFormatOptions) {
        return new Intl.DateTimeFormat(locale, { hour: "numeric", minute: "2-digit", ...options }).format(
          new Date(value),
        );
      },
      number(value: number, options?: Intl.NumberFormatOptions) {
        return new Intl.NumberFormat(locale, options).format(value);
      },
      currency(value: number, currency = "USD", options?: Intl.NumberFormatOptions) {
        return new Intl.NumberFormat(locale, {
          style: "currency",
          currency,
          ...options,
        }).format(value);
      },
      relativeTime(value: number, unit: Intl.RelativeTimeFormatUnit, options?: Intl.RelativeTimeFormatOptions) {
        return new Intl.RelativeTimeFormat(locale, { numeric: "auto", ...options }).format(value, unit);
      },
      bytes(bytes: number) {
        if (!Number.isFinite(bytes) || bytes < 0) {
          return `0 B`;
        }
        if (bytes < 1024) {
          return `${new Intl.NumberFormat(locale).format(bytes)} B`;
        }
        const units = ["KB", "MB", "GB", "TB"] as const;
        let value = bytes / 1024;
        let unitIndex = 0;
        while (value >= 1024 && unitIndex < units.length - 1) {
          value /= 1024;
          unitIndex += 1;
        }
        return `${new Intl.NumberFormat(locale, {
          maximumFractionDigits: value >= 10 ? 0 : 1,
        }).format(value)} ${units[unitIndex]}`;
      },
    }),
    [locale],
  );
}
