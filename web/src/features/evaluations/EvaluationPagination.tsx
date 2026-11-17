import { ResourceListPagination } from '../../shared/ui/resource-list-pagination';
import type { useEvaluationCursor } from './useEvaluationCursor';

export function EvaluationPagination({
  cursor,
  nextPage,
  fetching,
}: {
  cursor: ReturnType<typeof useEvaluationCursor>;
  nextPage: string | null | undefined;
  fetching: boolean;
}) {
  if (cursor.pageIndex === 0 && !nextPage) return null;
  return (
    <ResourceListPagination
      currentPage={cursor.pageIndex + 1}
      totalPages={null}
      canPrevious={cursor.pageIndex > 0 && !fetching}
      canNext={Boolean(nextPage) && !fetching}
      onPrevious={cursor.previous}
      onNext={() => cursor.next(nextPage ?? null)}
    />
  );
}
