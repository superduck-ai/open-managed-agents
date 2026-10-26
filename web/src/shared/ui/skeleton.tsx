import { cn } from '@/shared/lib/utils';

function Skeleton({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="skeleton" className={cn('rounded-md bg-muted', className)} {...props} />;
}

export { Skeleton };
