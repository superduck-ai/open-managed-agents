import { ChevronLeft } from 'lucide-react';
import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/utils';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';
import { Badge } from '@/shared/ui/badge';
import { ButtonLink } from '@/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/shared/ui/empty';
import { Field, FieldLabel } from '@/shared/ui/field';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import type { IconComponent } from './model';

export function ConsolePageFrame({
  title,
  description,
  eyebrow,
  meta,
  actions,
  children,
}: {
  title: string;
  description?: string;
  eyebrow?: string;
  meta?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="space-y-5">
      <ResourcePageHeader
        title={title}
        description={description}
        actions={actions}
        titleAdornment={eyebrow ? <Badge>{eyebrow}</Badge> : undefined}
      />
      {meta ? <div>{meta}</div> : null}
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

export { CursorPagination, TableEmptyRow, TableErrorRow, TableLoadingRow } from '@/shared/ui/resource-table';

export function EmptyState({
  icon: Icon,
  title,
  body,
  children,
}: {
  icon: IconComponent;
  title: string;
  body?: string;
  children?: ReactNode;
}) {
  return (
    <Empty className="min-h-[260px] px-6 py-12">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Icon className="size-5" aria-hidden />
        </EmptyMedia>
        <EmptyTitle>
          <h2 className="text-lg font-semibold leading-7 text-foreground">{title}</h2>
        </EmptyTitle>
        {body ? <EmptyDescription className="max-w-[520px] text-muted-foreground">{body}</EmptyDescription> : null}
      </EmptyHeader>
      {children ? <EmptyContent>{children}</EmptyContent> : null}
    </Empty>
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
        <TableHeader className="text-xs text-muted-foreground">
          <TableRow className="border-b-[0.5px] border-border hover:bg-transparent">
            {columns.map((column) => (
              <TableHead key={column} className="px-5 py-3 text-muted-foreground">
                {column}
              </TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={row.join('|')} className="border-b-[0.5px] border-border text-foreground last:border-0">
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
        <div className="mt-5 text-2xl font-semibold leading-none text-foreground">{value}</div>
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
