import { AlertCircle, ChartNoAxesColumn, type LucideIcon } from 'lucide-react';
import { cn } from '../../shared/lib/utils';
import { Button } from '../../shared/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '../../shared/ui/empty';

export function ObservabilityStatus({
  tone = 'empty',
  title,
  description,
  actionLabel,
  onAction,
  icon,
  size = 'compact',
  className,
}: {
  tone?: 'empty' | 'error';
  title: string;
  description?: string;
  actionLabel?: string;
  onAction?: () => void;
  icon?: LucideIcon;
  size?: 'compact' | 'page';
  className?: string;
}) {
  const Icon = icon ?? (tone === 'error' ? AlertCircle : ChartNoAxesColumn);
  const compact = size === 'compact';
  return (
    <Empty
      role={tone === 'error' ? 'alert' : 'status'}
      className={cn('rounded-none border-0', compact ? 'min-h-0 flex-1 gap-2 p-2' : 'min-h-48 gap-4 p-6', className)}
    >
      <EmptyHeader className={compact ? 'gap-1.5' : undefined}>
        <EmptyMedia
          variant="icon"
          className={cn(
            'mb-0',
            compact && 'size-7',
            !compact && 'size-10 rounded-full border border-border bg-secondary',
            tone === 'error' && 'border-destructive/20 bg-destructive/10 text-destructive',
          )}
        >
          <Icon className={compact ? 'size-3.5' : 'size-5'} aria-hidden />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        {description ? (
          <EmptyDescription className={cn(compact && 'text-xs leading-5')}>{description}</EmptyDescription>
        ) : null}
      </EmptyHeader>
      {onAction && actionLabel ? (
        <EmptyContent>
          <Button type="button" size="sm" variant="outline" onClick={onAction}>
            {actionLabel}
          </Button>
        </EmptyContent>
      ) : null}
    </Empty>
  );
}
