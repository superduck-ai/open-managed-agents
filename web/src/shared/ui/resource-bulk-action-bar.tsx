import { X } from 'lucide-react';
import type { ReactNode } from 'react';

import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';

export function ResourceBulkActionBar({
  selectedCount,
  selectedLabel,
  onClear,
  children,
}: {
  selectedCount: number;
  selectedLabel?: string;
  onClear: () => void;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  if (selectedCount <= 0) {
    return null;
  }
  return (
    <div
      role="status"
      className="fixed bottom-6 left-1/2 z-20 flex -translate-x-1/2 items-center gap-2 rounded-xl border-[0.5px] border-border bg-background p-1.5 pl-3 text-sm shadow-lg transition-[left] duration-220 ease-out motion-reduce:transition-none md:left-[calc(50%+var(--sidebar-width,256px)/2)]"
    >
      <span className="whitespace-nowrap font-medium tabular-nums text-foreground">
        {selectedLabel ?? msg('common.selectedCount', '{count} selected', { count: selectedCount })}
      </span>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={msg('common.clearSelection', 'Clear selection')}
        onClick={onClear}
      >
        <X className="size-3.5" aria-hidden />
      </Button>
      <span className="mx-1 h-5 w-[0.5px] bg-border" aria-hidden />
      <div className="flex items-center gap-1">{children}</div>
    </div>
  );
}
