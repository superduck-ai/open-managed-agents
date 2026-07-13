import * as React from 'react';
import { cva, type VariantProps } from 'class-variance-authority';

import { cn } from '@/shared/lib/utils';

function BubbleGroup({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="bubble-group" className={cn('flex min-w-0 flex-col gap-2', className)} {...props} />;
}

const bubbleVariants = cva(
  'group/bubble relative flex w-fit min-w-0 flex-col gap-1 group-data-[align=end]/message:self-end data-[align=end]:self-end',
  {
    variants: {
      variant: {
        default: 'max-w-[80%]',
        secondary: 'max-w-[80%]',
        muted: 'max-w-[80%]',
        tinted: 'max-w-[80%]',
        outline: 'max-w-[80%]',
        ghost: 'max-w-full',
        destructive: 'max-w-full',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
);

function Bubble({
  variant = 'default',
  align = 'start',
  className,
  ...props
}: React.ComponentProps<'div'> &
  VariantProps<typeof bubbleVariants> & {
    align?: 'start' | 'end';
  }) {
  return (
    <div
      data-slot="bubble"
      data-variant={variant}
      data-align={align}
      className={cn(bubbleVariants({ variant }), className)}
      {...props}
    />
  );
}

function BubbleContent({ className, ...props }: React.ComponentProps<'div'>) {
  return (
    <div
      data-slot="bubble-content"
      className={cn(
        'w-fit max-w-full min-w-0 overflow-hidden rounded-3xl border border-transparent px-3 py-2.5 text-sm leading-relaxed break-words whitespace-pre-wrap',
        'group-data-[align=end]/bubble:self-end',
        'group-data-[variant=default]/bubble:bg-primary group-data-[variant=default]/bubble:text-primary-foreground',
        'group-data-[variant=secondary]/bubble:bg-secondary group-data-[variant=secondary]/bubble:text-secondary-foreground',
        'group-data-[variant=muted]/bubble:bg-muted group-data-[variant=muted]/bubble:text-foreground',
        'group-data-[variant=tinted]/bubble:bg-[oklch(from_var(--primary)_0.93_calc(c*0.4)_h)] group-data-[variant=tinted]/bubble:text-foreground dark:group-data-[variant=tinted]/bubble:bg-[oklch(from_var(--primary)_0.3_calc(c*0.4)_h)]',
        'group-data-[variant=outline]/bubble:border-border group-data-[variant=outline]/bubble:bg-background group-data-[variant=outline]/bubble:text-foreground',
        'group-data-[variant=ghost]/bubble:rounded-none group-data-[variant=ghost]/bubble:border-transparent group-data-[variant=ghost]/bubble:bg-transparent group-data-[variant=ghost]/bubble:px-0 group-data-[variant=ghost]/bubble:py-0 group-data-[variant=ghost]/bubble:text-foreground',
        'group-data-[variant=destructive]/bubble:bg-destructive/10 group-data-[variant=destructive]/bubble:text-destructive dark:group-data-[variant=destructive]/bubble:bg-destructive/20',
        '[button]:text-left [button,a]:outline-none [button,a]:transition-colors [button,a]:focus-visible:border-ring [button,a]:focus-visible:ring-3 [button,a]:focus-visible:ring-ring/30',
        className,
      )}
      {...props}
    />
  );
}

const bubbleReactionsVariants = cva(
  'absolute z-10 flex w-fit shrink-0 items-center justify-center gap-1 rounded-full bg-muted px-1.5 py-0.5 text-sm ring-3 ring-card',
  {
    variants: {
      side: {
        top: 'top-0 -translate-y-3/4',
        bottom: 'bottom-0 translate-y-3/4',
      },
      align: {
        start: 'left-3',
        end: 'right-3',
      },
    },
    defaultVariants: {
      side: 'bottom',
      align: 'end',
    },
  },
);

function BubbleReactions({
  side = 'bottom',
  align = 'end',
  className,
  ...props
}: React.ComponentProps<'div'> & {
  align?: 'start' | 'end';
  side?: 'top' | 'bottom';
}) {
  return (
    <div
      data-slot="bubble-reactions"
      data-align={align}
      data-side={side}
      className={cn(bubbleReactionsVariants({ side, align }), className)}
      {...props}
    />
  );
}

export { BubbleGroup, Bubble, BubbleContent, BubbleReactions };
