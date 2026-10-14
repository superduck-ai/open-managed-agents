import { AlertCircle, RefreshCw } from 'lucide-react';
import type { ReactNode } from 'react';

import { Alert, AlertDescription, AlertTitle } from '@/shared/ui/alert';
import { Button } from '@/shared/ui/button';
import { ResourceListPagination } from '@/shared/ui/resource-list-pagination';
import { TableCell, TableRow } from '@/shared/ui/table';

export function CursorPagination({
  updatingLabel,
  canPrevious,
  canNext,
  isUpdating,
  currentPage,
  totalPages,
  onPrevious,
  onNext,
}: {
  updatingLabel: string;
  canPrevious: boolean;
  canNext: boolean;
  isUpdating: boolean;
  currentPage: number;
  totalPages: number | null;
  onPrevious: () => void;
  onNext: () => void;
}) {
  return (
    <ResourceListPagination
      currentPage={currentPage}
      totalPages={totalPages}
      canPrevious={canPrevious}
      canNext={canNext}
      onPrevious={onPrevious}
      onNext={onNext}
      updatingLabel={isUpdating ? updatingLabel : undefined}
    />
  );
}

export function TableLoadingRow({ colSpan, label }: { colSpan: number; label: string }) {
  return (
    <TableRow className="border-b border-border">
      <TableCell colSpan={colSpan} className="h-24 px-3 py-6 text-sm text-muted-foreground">
        <span className="inline-flex items-center gap-2">
          <RefreshCw className="size-3.5 animate-spin" aria-hidden />
          {label}
        </span>
      </TableCell>
    </TableRow>
  );
}

export function TableErrorRow({
  colSpan,
  title,
  message,
  retryLabel,
  onRetry,
}: {
  colSpan: number;
  title: string;
  message: string;
  retryLabel: string;
  onRetry: () => void;
}) {
  return (
    <TableRow className="border-b border-border">
      <TableCell colSpan={colSpan} className="h-28 px-3 py-6">
        <Alert variant="destructive" className="max-w-xl">
          <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <AlertTitle>{title}</AlertTitle>
          <AlertDescription>
            <p>{message}</p>
            <Button type="button" size="sm" variant="outline" className="mt-3" onClick={onRetry}>
              <RefreshCw className="size-3.5" aria-hidden />
              {retryLabel}
            </Button>
          </AlertDescription>
        </Alert>
      </TableCell>
    </TableRow>
  );
}

export function TableEmptyRow({ colSpan, children }: { colSpan: number; children: ReactNode }) {
  return (
    <TableRow className="border-b border-border">
      <TableCell colSpan={colSpan} className="h-24 px-3 py-6 text-sm text-muted-foreground">
        {children}
      </TableCell>
    </TableRow>
  );
}
