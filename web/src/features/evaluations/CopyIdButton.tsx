import { useState } from 'react';

import { cn } from '@/shared/lib/utils';

import { useI18n } from '../../shared/i18n';

export function CopyIdButton({ id, className }: { id: string; className?: string }) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      className={cn(
        'block w-full truncate text-left font-mono text-xs text-muted-foreground hover:text-foreground',
        className,
      )}
      aria-label={msg('evaluations.copyId', 'Copy {id}', { id })}
      onClick={() => {
        const write = navigator.clipboard?.writeText(id);
        if (!write) {
          return;
        }
        void write.then(() => {
          setCopied(true);
          window.setTimeout(() => setCopied(false), 1200);
        });
      }}
    >
      {copied ? msg('evaluations.copied', 'Copied') : id}
    </button>
  );
}
