'use client';

import { Separator as SeparatorPrimitive } from '@base-ui/react/separator';

import { cn } from '@/shared/lib/utils';

function Separator({ className, orientation = 'horizontal', ...props }: SeparatorPrimitive.Props) {
  return (
    <SeparatorPrimitive
      data-slot="separator"
      orientation={orientation}
      className={cn(
        'shrink-0 bg-border data-horizontal:h-[0.5px] data-horizontal:w-full data-vertical:w-[0.5px] data-vertical:self-stretch',
        className,
      )}
      {...props}
    />
  );
}

export { Separator };
