import type { ReactNode } from 'react';

import { cn } from '@/shared/lib/utils';
import { Badge } from '@/shared/ui/badge';

export type ResourceStatusTone = 'success' | 'warning' | 'danger' | 'neutral' | 'info';

const toneClassNames: Record<ResourceStatusTone, string> = {
  success: 'status-success',
  warning: 'status-warning',
  danger: 'status-danger',
  neutral: 'border-border bg-secondary text-secondary-foreground',
  info: 'border-border bg-secondary text-foreground',
};

export function ResourceStatusBadge({
  tone = 'neutral',
  className,
  children,
}: {
  tone?: ResourceStatusTone;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Badge variant="secondary" className={cn('rounded-md leading-none', toneClassNames[tone], className)}>
      {children}
    </Badge>
  );
}
