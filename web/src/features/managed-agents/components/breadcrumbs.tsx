import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '../../../shared/ui/breadcrumb';
import { ArrowLeft } from 'lucide-react';
import { type ReactNode } from 'react';

export function ManagedDetailBreadcrumb({
  listHref,
  listLabel,
  currentLabel,
  className,
  currentClassName,
  showBackIcon = false
}: {
  listHref: string;
  listLabel: string;
  currentLabel?: ReactNode;
  className?: string;
  currentClassName?: string;
  showBackIcon?: boolean;
}) {
  const { msg } = useI18n();

  return (
    <Breadcrumb aria-label={msg('navigation.breadcrumb', 'Breadcrumb')} className={className}>
      <BreadcrumbList className="min-w-0">
        <BreadcrumbItem>
          <BreadcrumbLink href={listHref} className={cn(showBackIcon && 'inline-flex items-center gap-2')}>
            {showBackIcon ? <ArrowLeft className="size-4" aria-hidden /> : null}
            {listLabel}
          </BreadcrumbLink>
        </BreadcrumbItem>
        {currentLabel ? (
          <>
            <BreadcrumbSeparator />
            <BreadcrumbItem className="min-w-0">
              <BreadcrumbPage className={cn('truncate', currentClassName)}>{currentLabel}</BreadcrumbPage>
            </BreadcrumbItem>
          </>
        ) : null}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
