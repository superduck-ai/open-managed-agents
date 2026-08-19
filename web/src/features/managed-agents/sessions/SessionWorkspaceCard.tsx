import type { ComponentProps } from 'react';
import { cn } from '../../../shared/lib/utils';
import { Card } from '../../../shared/ui/card';

export function SessionWorkspaceCard({ className, ...props }: ComponentProps<typeof Card>) {
  return (
    <Card
      data-session-workspace-card=""
      className={cn('w-full border border-border shadow-sm ring-0', className)}
      {...props}
    />
  );
}
