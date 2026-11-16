import { useState, type ReactNode } from 'react';
import { ChevronDown } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';

export function OptionalCredentialFields({
  title,
  defaultOpen = false,
  open: controlledOpen,
  onOpenChange,
  children,
}: {
  title: string;
  defaultOpen?: boolean;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  const [internalOpen, setOpen] = useState(defaultOpen);
  const open = controlledOpen ?? internalOpen;
  return (
    <Collapsible
      open={open}
      onOpenChange={onOpenChange ?? setOpen}
      className="min-w-0 border-t-[0.5px] border-border pt-3"
    >
      <CollapsibleTrigger
        type="button"
        className="flex min-h-9 w-full items-center gap-2 text-left text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[0.5px] focus-visible:ring-inset focus-visible:ring-foreground"
      >
        <ChevronDown
          className={`size-4 shrink-0 transition-transform duration-200 ease-out motion-reduce:transition-none ${open ? '' : '-rotate-90'}`}
          aria-hidden
        />
        <span className="min-w-0 flex-1 font-medium text-foreground">{title}</span>
        <span className="text-xs text-muted-foreground">
          {msg('managedAgents.credentialVaults.credentialDialog.optional', 'Optional')}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-3 pt-3">{children}</CollapsibleContent>
    </Collapsible>
  );
}
