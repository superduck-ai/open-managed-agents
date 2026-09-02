import { Copy } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { copyText } from '../../../shared/lib/clipboard';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '../../../shared/ui/popover';
import { toast } from '../../../shared/ui/sonner';
import { TableCell } from '../../../shared/ui/table';

export function TraceTextPreview({ value, className }: { value?: string; className?: string }) {
  const { msg } = useI18n();
  const text = value?.trim() ?? '';
  if (!text) {
    return <span className={cn('truncate text-muted-foreground', className)}>—</span>;
  }
  const copyFullText = () => {
    void copyText(text).then(() => toast.success(msg('common.copied', 'Copied')));
  };
  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            className={cn('min-w-0 truncate text-left hover:bg-muted/80', className)}
            aria-label={msg('observability.traces.showFull', 'Show full text')}
            onClick={(event) => event.stopPropagation()}
          />
        }
      >
        {text}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="flex w-[min(42rem,calc(100vw-2rem))] max-w-none flex-col gap-2"
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-center justify-end">
          <Button type="button" size="xs" variant="outline" onClick={copyFullText}>
            <Copy className="size-3" aria-hidden />
            {msg('common.copy', 'Copy')}
          </Button>
        </div>
        <p className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-words font-mono text-xs leading-5">{text}</p>
      </PopoverContent>
    </Popover>
  );
}

export function TracePreviewTableCell({ value }: { value?: string }) {
  const text = value?.trim() ?? '';
  if (!text) {
    return <TableCell className="max-w-0 overflow-hidden text-ellipsis text-muted-foreground">—</TableCell>;
  }
  return (
    <TableCell className="max-w-0 overflow-hidden p-0">
      <TraceTextPreview value={text} className="block w-full px-2 py-2" />
    </TableCell>
  );
}

export function TraceIOPanels({
  input,
  output,
  className,
  contentClassName,
}: {
  input: string;
  output: string;
  className?: string;
  contentClassName?: string;
}) {
  const { msg } = useI18n();
  return (
    <div className={cn('grid min-w-0 gap-3', className)}>
      <TraceIOPanel
        label={msg('observability.trace.input', 'Input')}
        value={input}
        contentClassName={contentClassName}
      />
      <TraceIOPanel
        label={msg('observability.trace.output', 'Output')}
        value={output}
        contentClassName={contentClassName}
      />
    </div>
  );
}

function TraceIOPanel({ label, value, contentClassName }: { label: string; value: string; contentClassName?: string }) {
  const { msg } = useI18n();
  const text = value.trim();
  const copyValue = () => {
    void copyText(text).then(() => toast.success(msg('common.copied', 'Copied')));
  };
  return (
    <section className="flex min-w-0 flex-col overflow-hidden rounded-md border border-border">
      <div className="flex h-7 shrink-0 items-center justify-between gap-2 border-b border-border bg-muted/40 pl-2 pr-1">
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
        {text ? (
          <Button
            type="button"
            size="icon-xs"
            variant="ghost"
            aria-label={msg('common.copy', 'Copy')}
            onClick={copyValue}
          >
            <Copy className="size-3" aria-hidden />
          </Button>
        ) : null}
      </div>
      {text ? (
        <p
          className={cn(
            'subtle-scrollbar min-h-0 overflow-auto whitespace-pre-wrap break-words px-2 py-1.5 font-mono text-xs leading-5',
            contentClassName,
          )}
        >
          {text}
        </p>
      ) : (
        <p className="px-2 py-1.5 text-xs text-muted-foreground">—</p>
      )}
    </section>
  );
}
