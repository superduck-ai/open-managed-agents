import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';

const pageSizeOptions = [10, 20] as const;

export function ChannelTablePager({
  page,
  pageCount,
  total,
  pageSize,
  onPage,
  onPageSize,
}: {
  page: number;
  pageCount: number;
  total: number;
  pageSize: number;
  onPage: (page: number) => void;
  onPageSize: (pageSize: number) => void;
}) {
  const { msg } = useI18n();
  const sizeLabel = (size: number) => msg('managedAgents.channels.perPage', '{count} 条/页', { count: size });
  return (
    <div className="flex items-center justify-end gap-3 border-t border-border px-4 py-3 text-sm text-muted-foreground">
      <span>{msg('managedAgents.channels.sessionCount', '共 {count} 条', { count: total })}</span>
      <Button
        type="button"
        variant="outline"
        size="icon"
        disabled={page <= 1}
        aria-label={msg('pagination.previousPage', '上一页')}
        onClick={() => onPage(page - 1)}
      >
        <ChevronLeft className="size-4" />
      </Button>
      <span className="inline-flex h-8 min-w-8 items-center justify-center rounded-md border border-border px-2 text-foreground">
        {page}
      </span>
      <Button
        type="button"
        variant="outline"
        size="icon"
        disabled={page >= pageCount}
        aria-label={msg('pagination.nextPage', '下一页')}
        onClick={() => onPage(page + 1)}
      >
        <ChevronRight className="size-4" />
      </Button>
      <Select
        value={String(pageSize)}
        onValueChange={(value) => onPageSize(Number(value) || pageSize)}
        items={pageSizeOptions.map((size) => ({ value: String(size), label: sizeLabel(size) }))}
      >
        <SelectTrigger className="h-8 w-[108px]" aria-label={sizeLabel(pageSize)}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {pageSizeOptions.map((size) => (
            <SelectItem key={size} value={String(size)}>
              {sizeLabel(size)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
