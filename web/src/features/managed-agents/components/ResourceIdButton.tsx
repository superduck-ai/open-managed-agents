import { Check, Copy } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { copyText } from '../../../shared/lib/clipboard';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import { toast } from '../../../shared/ui/sonner';
import { Tooltip, TooltipContent, TooltipTrigger } from '../../../shared/ui/tooltip';

export function ResourceIdButton({
  value,
  displayValue = value,
  className,
  wrap = false,
}: {
  value: string;
  displayValue?: string;
  className?: string;
  wrap?: boolean;
}) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    },
    [],
  );

  const handleCopy = async () => {
    try {
      await copyText(value);
      setCopied(true);
      toast.success(msg('common.idCopied', 'ID copied'));
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error(msg('common.copyFailed', 'Copy failed'));
    }
  };

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className={cn(
              'h-auto min-h-8 min-w-0 max-w-full justify-start gap-1.5 px-1.5 py-1 text-muted-foreground',
              className,
            )}
            aria-label={msg('managedAgents.common.copyIdValue', 'Copy {id}', { id: value })}
            onClick={(event) => {
              event.stopPropagation();
              void handleCopy();
            }}
          />
        }
      >
        <span className={cn('min-w-0 font-mono text-xs', wrap ? 'whitespace-normal break-all text-left' : 'truncate')}>
          {displayValue}
        </span>
        {copied ? (
          <Check className="size-3.5 shrink-0" aria-hidden />
        ) : (
          <Copy className="size-3.5 shrink-0" aria-hidden />
        )}
      </TooltipTrigger>
      <TooltipContent className="max-w-[min(32rem,calc(100vw-2rem))]">
        <p className="break-all font-mono">{value}</p>
        <p className="mt-1">{msg('common.clickToCopy', 'Click to copy the full ID')}</p>
      </TooltipContent>
    </Tooltip>
  );
}
