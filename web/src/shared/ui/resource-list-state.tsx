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
  body?: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <div className="grid min-h-[320px] min-w-0 place-items-center text-center">
      <div className="w-full min-w-0 max-w-[360px] [overflow-wrap:anywhere]">
        <Icon className="mx-auto mb-4 size-14 stroke-[1.2] text-foreground" aria-hidden />
        <div className="text-sm font-medium text-foreground">{title}</div>
        {body ? <p className="mt-3 text-sm leading-5 text-muted-foreground">{body}</p> : null}
        {actionLabel && onAction ? (
          <Button type="button" variant="outline" className="mt-4" onClick={onAction}>
            {actionLabel}
          </Button>
        ) : null}
      </div>
    </div>
  );
}
