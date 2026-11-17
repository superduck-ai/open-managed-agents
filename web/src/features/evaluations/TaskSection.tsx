import type { ReactNode } from 'react';

export function TaskSection({
  index,
  title,
  hint,
  actions,
  children,
}: {
  index?: string;
  title: string;
  hint: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="grid gap-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex items-start gap-3">
          {index ? (
            <span className="mt-0.5 rounded-md bg-muted px-1.5 py-0.5 font-mono text-xs text-muted-foreground">
              {index}
            </span>
          ) : null}
          <div>
            <h2 className="text-base leading-6 font-medium">{title}</h2>
            <p className="text-xs text-muted-foreground">{hint}</p>
          </div>
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}
