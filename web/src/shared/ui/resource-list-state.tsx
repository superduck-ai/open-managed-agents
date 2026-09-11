import { type ComponentType } from 'react';

import { Button } from '@/shared/ui/button';

export function ResourceListState({
  icon: Icon,
  title,
  body,
  actionLabel,
  onAction,
}: {
  icon: ComponentType<{ className?: string; 'aria-hidden'?: boolean }>;
  title: string;
  body: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <div className="grid min-h-[320px] place-items-center text-center">
      <div className="max-w-[360px]">
        <Icon className="mx-auto mb-4 size-12 stroke-[1.3] text-foreground" aria-hidden />
        <div className="text-sm font-semibold text-foreground">{title}</div>
        <p className="mt-3 text-sm leading-5 text-muted-foreground">{body}</p>
        {actionLabel && onAction ? (
          <Button type="button" variant="outline" className="mt-4" onClick={onAction}>
            {actionLabel}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
