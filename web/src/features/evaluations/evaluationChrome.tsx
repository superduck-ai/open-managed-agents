import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';

import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';

export function EvaluationEmpty({
  icon: Icon,
  title,
  body,
  action,
  onAction,
}: {
  icon: LucideIcon;
  title: string;
  body: string;
  action: string;
  onAction: () => void;
}) {
  return (
    <div className="grid min-h-[280px] place-items-center rounded-xl border border-border bg-background px-6 py-16 text-center">
      <div className="max-w-[360px]">
        <Icon className="mx-auto mb-4 size-8 text-muted-foreground" aria-hidden />
        <div className="text-sm font-medium">{title}</div>
        <p className="mt-2 text-sm leading-5 text-muted-foreground">{body}</p>
        <Button type="button" className="mt-4" onClick={onAction}>
          {action}
        </Button>
      </div>
    </div>
  );
}

export function EvaluationSearch({
  value,
  placeholder,
  onChange,
  filters,
}: {
  value: string;
  placeholder: string;
  onChange: (value: string) => void;
  filters?: ReactNode;
}) {
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2">
      <Input
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="h-9 max-w-xs bg-background"
      />
      {filters}
    </div>
  );
}

export function shortenId(id: string) {
  if (id.length <= 20) return id;
  return `${id.slice(0, 9)}…${id.slice(-7)}`;
}

export function formatEvaluationTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (part: number) => String(part).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}
