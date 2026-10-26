import type { ComponentType, ReactNode } from 'react';
import { Info } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
type IconComponent = ComponentType<{ className?: string; 'aria-hidden'?: boolean }>;
type FilterOption = { value: string; label: string };

export function AnalyticsPageRoot({
  title,
  icon: Icon,
  children,
}: {
  title: string;
  icon: IconComponent;
  children: ReactNode;
}) {
  return (
    <section className="space-y-5" data-testid="analytics-page">
      <div className="flex items-center gap-3">
        <div className="grid size-9 place-items-center rounded-lg border border-border bg-secondary text-foreground">
          <Icon className="size-4.5" aria-hidden />
        </div>
        <h1 className="text-[30px] font-semibold leading-tight text-foreground">{title}</h1>
      </div>
      {children}
    </section>
  );
}

export function FilterBar({ children, compact = false }: { children: ReactNode; compact?: boolean }) {
  return (
    <div className={compact ? 'flex flex-wrap items-center gap-2' : 'flex flex-wrap items-center gap-2'}>
      {children}
    </div>
  );
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
      <SelectTrigger
        aria-label={ariaLabel}
        className="h-9 border-border bg-background px-2.5 text-foreground shadow-sm"
      >
        <SelectValue>
          {label ? <span className="text-muted-foreground/70">{label}</span> : null}
          <span>{selectedLabel}</span>
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
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-foreground">
          {title}
          <Info className="size-4 text-muted-foreground/70" aria-hidden />
        </CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <div className="mt-6 text-[30px] font-semibold leading-none text-foreground">{value}</div>
        {detail ? <div className="mt-3 text-sm text-muted-foreground">{detail}</div> : null}
      </CardContent>
    </Card>
  );
}
