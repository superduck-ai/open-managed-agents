import type { ReactNode } from 'react';

import { useI18n } from '../../shared/i18n';

export const casePageSize = 20;

export function casePageSlice<T>(items: T[], page: number) {
  const pageCount = Math.max(1, Math.ceil(items.length / casePageSize));
  const safePage = Math.min(Math.max(page, 0), pageCount - 1);
  return {
    page: safePage,
    pageCount,
    items: items.slice(safePage * casePageSize, safePage * casePageSize + casePageSize),
  };
}

export function casePageIndexes(page: number, pageCount: number): Array<number | 'gap'> {
  if (pageCount <= 5) {
    return Array.from({ length: pageCount }, (_, index) => index);
  }
  const wanted = new Set<number>([0, pageCount - 1, page - 1, page, page + 1]);
  if (page <= 1) {
    for (let index = 0; index < 5; index += 1) {
      wanted.add(index);
    }
  }
  const pages = [...wanted].filter((index) => index >= 0 && index < pageCount).sort((left, right) => left - right);
  const indexes: Array<number | 'gap'> = [];
  for (const index of pages) {
    const previous = indexes[indexes.length - 1];
    if (typeof previous === 'number' && index - previous > 1) {
      indexes.push('gap');
    }
    indexes.push(index);
  }
  return indexes;
}

export function CaseListPager({
  total,
  page,
  pageCount,
  onPage,
}: {
  total: number;
  page: number;
  pageCount: number;
  onPage: (page: number) => void;
}) {
  const { msg } = useI18n();
  if (total <= 0) {
    return null;
  }
  return (
    <div className="flex items-center justify-between gap-3 border-t px-4 py-3">
      <p className="text-sm text-muted-foreground">{msg('evaluations.caseTotal', '{count} cases', { count: total })}</p>
      <nav aria-label={msg('evaluations.caseList', 'Cases')} className="flex items-center gap-1">
        <PagerButton disabled={page === 0} onClick={() => onPage(page - 1)}>
          {msg('evaluations.pagePrevious', 'Previous')}
        </PagerButton>
        {casePageIndexes(page, pageCount).map((item, index) =>
          item === 'gap' ? (
            <span key={`gap-${index}`} className="px-1 text-sm text-muted-foreground">
              …
            </span>
          ) : (
            <PagerButton
              key={item}
              current={item === page}
              label={msg('evaluations.pageNumber', 'Page {page}', { page: item + 1 })}
              onClick={() => onPage(item)}
            >
              {item + 1}
            </PagerButton>
          ),
        )}
        <PagerButton disabled={page >= pageCount - 1} onClick={() => onPage(page + 1)}>
          {msg('evaluations.pageNext', 'Next')}
        </PagerButton>
      </nav>
    </div>
  );
}

function PagerButton({
  children,
  current = false,
  disabled = false,
  label,
  onClick,
}: {
  children: ReactNode;
  current?: boolean;
  disabled?: boolean;
  label?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-label={label}
      aria-current={current ? 'page' : undefined}
      className={
        current
          ? 'inline-flex h-8 min-w-8 items-center justify-center rounded-md bg-foreground px-2 text-sm text-background'
          : 'inline-flex h-8 min-w-8 items-center justify-center rounded-md px-2 text-sm text-muted-foreground hover:bg-muted hover:text-foreground disabled:pointer-events-none disabled:opacity-40'
      }
      onClick={onClick}
    >
      {children}
    </button>
  );
}
