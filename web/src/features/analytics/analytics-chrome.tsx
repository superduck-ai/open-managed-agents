import type { ReactNode } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';
type FilterOption = { value: string; label: string };

export function AnalyticsPageRoot({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-5" data-testid="analytics-page">
      <ResourcePageHeader title={title} />
      {children}
    </section>
  );
}

export function FilterBar({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-2">{children}</div>;
}

export function FilterControl({
  label,
  value,
  options,
  disabled = false,
  onValueChange,
}: {
  label?: string;
  value: string;
  options: FilterOption[];
  disabled?: boolean;
  onValueChange: (value: string) => void;
}) {
  const selectedLabel = options.find((option) => option.value === value)?.label ?? value;
  const ariaLabel = label ? `${label}: ${selectedLabel}` : selectedLabel;

  return (
    <Select<string>
      value={value}
      items={options}
      disabled={disabled}
      onValueChange={(nextValue) => {
        if (nextValue !== null) {
          onValueChange(nextValue);
        }
      }}
    >
      <SelectTrigger aria-label={ariaLabel} className="h-9 max-w-full px-2.5">
        <SelectValue className="flex items-center gap-2">
          {label ? <span className="text-muted-foreground">{label}</span> : null}
          <span className="truncate">{selectedLabel}</span>
        </SelectValue>
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value} label={option.label}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function MetricPanel({ title, value, detail }: { title: string; value: string; detail?: string }) {
  return (
    <Card role="article" className="min-h-[126px] gap-0 rounded-lg p-5">
      <CardHeader className="p-0">
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <div className="mt-6 text-2xl font-semibold leading-none text-foreground">{value}</div>
        {detail ? <div className="mt-3 text-sm text-muted-foreground">{detail}</div> : null}
      </CardContent>
    </Card>
  );
}
