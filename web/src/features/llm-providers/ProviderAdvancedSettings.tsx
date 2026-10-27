import type { ReactNode } from 'react';
import { ChevronRight } from 'lucide-react';
import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../shared/ui/collapsible';

export function ProviderAdvancedSettings({
  enabled,
  open,
  onOpenChange,
  children,
}: {
  enabled: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  if (!enabled) return <>{children}</>;
  return (
    <Collapsible open={open} onOpenChange={onOpenChange} className="border-t-[0.5px] border-border pt-3">
      <CollapsibleTrigger
        render={<Button type="button" variant="ghost" />}
        className="group w-full justify-start gap-2 px-0 hover:bg-transparent"
      >
        <ChevronRight
          aria-hidden
          className="transition-transform group-aria-expanded:rotate-90 motion-reduce:transition-none"
        />
        {msg('llmModels.customSettings', 'Custom settings')}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="grid min-w-0 gap-5 pt-4 pb-1 pl-6">{children}</div>
      </CollapsibleContent>
    </Collapsible>
  );
}
