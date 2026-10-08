import { Check, Copy } from 'lucide-react';
import { useState } from 'react';

import { useI18n } from '@/shared/i18n';
import { copyText } from '@/shared/lib/clipboard';
import { Button } from '@/shared/ui/button';

export function CopyButton({ value, label, disabled = false }: { value: string; label: string; disabled?: boolean }) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await copyText(value);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 900);
  };

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      disabled={disabled}
      aria-label={copied ? msg('common.copied', 'Copied') : label}
      className="text-foreground hover:bg-accent hover:text-foreground"
      onClick={handleCopy}
    >
      {copied ? <Check className="size-4" aria-hidden /> : <Copy className="size-4" aria-hidden />}
    </Button>
  );
}
