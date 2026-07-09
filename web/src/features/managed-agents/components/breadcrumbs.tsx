import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '../../../shared/ui/breadcrumb';
import { type ReactNode } from 'react';
import { handleInternalLinkClick } from '../utils';

export function ManagedDetailBreadcrumb({
  listHref,
  listLabel,
  currentLabel,
  className,
  currentClassName
}: {
  listHref: string;
  listLabel: string;
  currentLabel?: ReactNode;
  className?: string;
  currentClassName?: string;
}) {
  const { msg } = useI18n();

  return (
    <Breadcrumb aria-label={msg('navigation.breadcrumb', 'Breadcrumb')} className={className}>
      <BreadcrumbList className="min-w-0">
        <BreadcrumbItem>
          <BreadcrumbLink href={listHref} onClick={(event) => handleInternalLinkClick(event, listHref)}>
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
