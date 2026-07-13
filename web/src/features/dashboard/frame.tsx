import { AlertCircle, ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/utils';
import { Alert, AlertDescription, AlertTitle } from '@/shared/ui/alert';
import { Badge } from '@/shared/ui/badge';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/shared/ui/empty';
import { Field, FieldLabel } from '@/shared/ui/field';
import { Pagination, PaginationContent, PaginationItem } from '@/shared/ui/pagination';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import type { IconComponent } from './model';

export function ConsolePageFrame({
  title,
  icon: Icon,
  description,
  eyebrow,
  meta,
  actions,
  children,
}: {
  title: string;
  icon: IconComponent;
  description?: string;
  eyebrow?: string;
  meta?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-5">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 items-start gap-3">
          <div className="grid size-10 shrink-0 place-items-center rounded-lg border border-border bg-secondary">
            <Icon className="size-5 text-muted-foreground" aria-hidden />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-[28px] font-semibold leading-tight text-foreground">{title}</h1>
              {eyebrow ? <Badge>{eyebrow}</Badge> : null}
            </div>
            {description ? (
              <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">{description}</p>
            ) : null}
            {meta ? <div className="mt-3">{meta}</div> : null}
          </div>
        </div>
        {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
      </div>
      {children}
    </section>
  );
}

export function PrimaryAction({ href, icon: Icon, label }: { href: string; icon: IconComponent; label: string }) {
  return (
    <ButtonLink href={href} size="lg">
      <Icon className="size-4" aria-hidden />
      {label}
    </ButtonLink>
  );
}

export function SecondaryAction({ href, icon: Icon, label }: { href: string; icon: IconComponent; label: string }) {
  const external = href.startsWith('http');
  return (
    <ButtonLink
      href={href}
      target={external ? '_blank' : undefined}
      rel={external ? 'noreferrer' : undefined}
      variant="outline"
      size="lg"
    >
      <Icon className="size-4" aria-hidden />
      {label}
    </ButtonLink>
  );
}

export function BackLink({ href, label }: { href: string; label: string }) {
  return (
    <ButtonLink href={href} variant="ghost" size="sm" className="-ml-2 text-muted-foreground hover:text-foreground">
      <ChevronLeft className="size-4" aria-hidden />
      {label}
    </ButtonLink>
  );
}

export function EmptyState({
  icon: Icon,
  title,
  body,
  action,
}: {
  icon: IconComponent;
  title: string;
  body: string;
  action?: string;
}) {
  return (
    <Empty className="min-h-[260px] rounded-lg border border-dashed border-border bg-card px-6 py-12">
      <EmptyHeader>
        <EmptyMedia
          variant="icon"
          className="size-12 rounded-full border border-border bg-secondary text-muted-foreground"
        >
          <Icon className="size-5" aria-hidden />
        </EmptyMedia>
        <EmptyTitle>
          <h2 className="text-[20px] font-semibold leading-7 text-foreground">{title}</h2>
        </EmptyTitle>
        <EmptyDescription className="max-w-[520px] text-muted-foreground">{body}</EmptyDescription>
      </EmptyHeader>
      {action ? (
        <EmptyContent>
          <Button type="button" variant="outline" size="lg">
            <RefreshCw className="size-4" aria-hidden />
            {action}
          </Button>
        </EmptyContent>
      ) : null}
    </Empty>
  );
}

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

export function DataTableCard({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <Card className={cn('overflow-hidden py-0', className)}>
      <CardContent className="p-0">{children}</CardContent>
    </Card>
  );
}

export function DataTable({ columns, rows }: { columns: string[]; rows: string[][] }) {
  return (
    <DataTableCard>
      <Table className="min-w-full text-left">
        <TableHeader className="text-xs text-muted-foreground/70">
          <TableRow className="border-b border-border hover:bg-transparent">
            {columns.map((column) => (
              <TableHead key={column} className="px-5 py-3 text-muted-foreground/70">
                {column}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={row.join('|')} className="border-b border-border text-foreground last:border-0">
              {row.map((cell, index) => (
                <TableCell key={`${cell}-${index}`} className="px-5 py-4 align-top">
                  {cell}
                </TableCell>
              ))}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </DataTableCard>
  );
}

export function NoticeCard({ icon: Icon, title, action }: { icon: IconComponent; title: string; action?: ReactNode }) {
  return (
    <Card className="flex-row flex-wrap items-center justify-between gap-4 rounded-lg p-4">
      <div className="flex items-center gap-3 text-sm text-foreground">
        <Icon className="size-4 text-muted-foreground" aria-hidden />
        {title}
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </Card>
  );
}

export function PanelCard({
  title,
  action,
  children,
  className,
  headerClassName,
  titleClassName,
  contentClassName,
}: {
  title: ReactNode;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
  headerClassName?: string;
  titleClassName?: string;
  contentClassName?: string;
}) {
  return (
    <Card className={cn('gap-0 rounded-lg p-4', className)}>
      <CardHeader className={cn('mb-4 flex flex-row items-center justify-between gap-3 p-0', headerClassName)}>
        <CardTitle className={cn('text-sm font-semibold text-foreground', titleClassName)}>{title}</CardTitle>
        {action}
      </CardHeader>
      <CardContent className={cn('p-0', contentClassName)}>{children}</CardContent>
    </Card>
  );
}

export function MetricTile({ title, value }: { title: string; value: string }) {
  return (
    <Card className="rounded-lg p-5">
      <CardHeader className="p-0">
        <CardTitle className="text-sm font-medium text-foreground">{title}</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <div className="mt-5 text-[30px] font-semibold leading-none text-foreground">{value}</div>
      </CardContent>
    </Card>
  );
}

export function SettingRow({
  title,
  body,
  detail,
  action,
}: {
  title: ReactNode;
  body: string;
  detail?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-4 p-5">
      <div>
        <h2 className="text-sm font-semibold text-foreground">{title}</h2>
        <p className="mt-1 text-sm leading-6 text-muted-foreground">{body}</p>
        {detail ? <div className="mt-2 text-sm font-medium text-foreground">{detail}</div> : null}
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}

export function ControlRow({ label, value }: { label: string; value: string }) {
  return (
    <Field className="gap-1">
      <FieldLabel className="text-xs font-medium text-muted-foreground">{label}</FieldLabel>
      <Select<string> value={value} items={[{ value, label: value }]} disabled>
        <SelectTrigger
          aria-label={label}
          className="h-9 w-full border-border bg-secondary px-3 text-sm text-foreground disabled:cursor-default disabled:opacity-100"
        >
          <SelectValue>{value}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectItem value={value} label={value}>
            {value}
          </SelectItem>
        </SelectContent>
      </Select>
    </Field>
  );
}
