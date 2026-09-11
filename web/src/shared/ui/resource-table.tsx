import { AlertCircle, ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react';
import type { ReactNode } from 'react';

import { Alert, AlertDescription, AlertTitle } from '@/shared/ui/alert';
import { Button } from '@/shared/ui/button';
import { Pagination, PaginationContent, PaginationItem } from '@/shared/ui/pagination';
import { TableCell, TableRow } from '@/shared/ui/table';

export function CursorPagination({
  previousLabel,
  nextLabel,
  updatingLabel,
  canPrevious,
  canNext,
  isUpdating,
  onPrevious,
  onNext,
}: {
  previousLabel: string;
  nextLabel: string;
  updatingLabel: string;
  canPrevious: boolean;
  canNext: boolean;
  isUpdating: boolean;
  onPrevious: () => void;
  onNext: () => void;
}) {
  return (
    <div className="mt-5 flex items-center gap-2">
      <Pagination className="mx-0 w-auto justify-start">
        <PaginationContent>
          <PaginationItem>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={previousLabel}
              className="text-muted-foreground"
              disabled={!canPrevious}
              onClick={onPrevious}
            >
              <ChevronLeft className="size-4" aria-hidden />
            </Button>
          </PaginationItem>
          <PaginationItem>
            <Button
              type="button"
              variant="ghost"
              size="icon"
              aria-label={nextLabel}
              className="text-muted-foreground"
              disabled={!canNext}
              onClick={onNext}
            >
              <ChevronRight className="size-4" aria-hidden />
            </Button>
          </PaginationItem>
        </PaginationContent>
      </Pagination>
      {isUpdating ? <span className="ml-2 text-xs text-muted-foreground/70">{updatingLabel}</span> : null}
    </div>
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
