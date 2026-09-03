'use client';

import { ScrollArea as ScrollAreaPrimitive } from '@base-ui/react/scroll-area';

import { cn } from '@/shared/lib/utils';

function ScrollAreaRoot({ className, ...props }: ScrollAreaPrimitive.Root.Props) {
  return (
    <ScrollAreaPrimitive.Root
      data-slot="scroll-area"
      className={cn('group/scroll-area relative min-h-0 min-w-0 overflow-hidden', className)}
      {...props}
    />
  );
}

function ScrollAreaViewport({ className, ...props }: ScrollAreaPrimitive.Viewport.Props) {
  return (
    <ScrollAreaPrimitive.Viewport
      data-slot="scroll-area-viewport"
      className={cn('size-full min-h-0 min-w-0 overscroll-contain outline-none', className)}
      {...props}
    />
  );
}

function ScrollAreaContent({ className, ...props }: ScrollAreaPrimitive.Content.Props) {
  return (
    <ScrollAreaPrimitive.Content
      data-slot="scroll-area-content"
      className={cn('min-h-full min-w-full', className)}
      {...props}
    />
  );
}

function ScrollAreaScrollbar({ className, orientation = 'vertical', ...props }: ScrollAreaPrimitive.Scrollbar.Props) {
  return (
    <ScrollAreaPrimitive.Scrollbar
      data-slot="scroll-area-scrollbar"
      orientation={orientation}
      className={cn(
        'z-10 flex touch-none p-0.5 opacity-0 transition-opacity duration-150 select-none data-hovering:opacity-100 data-scrolling:opacity-100 group-focus-within/scroll-area:opacity-100',
        'data-horizontal:h-2 data-horizontal:flex-col data-vertical:w-2',
        className,
      )}
      {...props}
    >
      <ScrollAreaThumb />
    </ScrollAreaPrimitive.Scrollbar>
  );
}

function ScrollAreaThumb({ className, ...props }: ScrollAreaPrimitive.Thumb.Props) {
  return (
    <ScrollAreaPrimitive.Thumb
      data-slot="scroll-area-thumb"
      className={cn('relative flex-1 rounded-full bg-muted-foreground/35 hover:bg-muted-foreground/50', className)}
      {...props}
    />
  );
}

function ScrollAreaCorner({ className, ...props }: ScrollAreaPrimitive.Corner.Props) {
  return (
    <ScrollAreaPrimitive.Corner data-slot="scroll-area-corner" className={cn('bg-transparent', className)} {...props} />
  );
}

function ScrollArea({ className, children, ...props }: ScrollAreaPrimitive.Root.Props) {
  return (
    <ScrollAreaRoot className={cn('size-full', className)} {...props}>
      <ScrollAreaViewport>
        <ScrollAreaContent>{children}</ScrollAreaContent>
      </ScrollAreaViewport>
      <ScrollAreaScrollbar />
      <ScrollAreaScrollbar orientation="horizontal" />
      <ScrollAreaCorner />
    </ScrollAreaRoot>
  );
}

export { ScrollArea };
