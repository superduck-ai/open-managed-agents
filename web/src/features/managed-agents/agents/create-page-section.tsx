import { ChevronRight, ExternalLink } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { useI18n } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/shared/ui/collapsible';

export function CreatePageSection({
  title,
  description,
  learnMore,
  summary,
  className,
  children,
}: {
  title: string;
  description: string;
  learnMore?: string;
  summary?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  return (
    <Collapsible open={open} onOpenChange={setOpen} className={cn('border-b border-border', className)}>
      <div className="grid gap-2 py-5 md:grid-cols-[244px_minmax(0,1fr)] md:gap-6">
        <CollapsibleTrigger className="flex items-center gap-2 rounded-md py-1 text-left text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <ChevronRight className={cn('size-4 shrink-0 transition-transform', open && 'rotate-90')} aria-hidden />
          {title}
          <span className="font-normal text-muted-foreground">
            {msg('managedAgents.common.optionalParen', '(optional)')}
          </span>
        </CollapsibleTrigger>
        {!open ? <div className="min-w-0 truncate py-1 text-sm text-muted-foreground">{summary}</div> : null}
      </div>
      <CollapsibleContent keepMounted>
        <div className="grid gap-4 pb-6 md:grid-cols-[244px_minmax(0,1fr)] md:gap-6">
          <div className="space-y-2 text-sm leading-5 text-muted-foreground">
            <p>{description}</p>
            {learnMore ? (
              <a
                href={learnMore}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 text-primary hover:underline"
              >
                {msg('common.learnMore', 'Learn more')}
                <ExternalLink className="size-3.5" aria-hidden />
              </a>
            ) : null}
          </div>
          <div className="min-w-0">{children}</div>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
