import { type ReactNode } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '../../../shared/ui/dialog';
import { X } from 'lucide-react';

export function AgentDialogFrame({
  onClose,
  label,
  title,
  description,
  className,
  ariaModal = false,
  children,
}: {
  onClose: () => void;
  label: string;
  title: ReactNode;
  description: ReactNode;
  className: string;
  ariaModal?: boolean;
  children: ReactNode;
}) {
  const { msg } = useI18n();
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        aria-modal={ariaModal ? 'true' : undefined}
        aria-label={label}
        className={className}
        showCloseButton={false}
      >
        <div className="flex h-full min-h-0 flex-col text-foreground">
          <DialogClose
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="absolute right-[18px] top-[18px] text-foreground hover:bg-accent"
              />
            }
          >
            <X className="size-[22px]" aria-hidden />
            <span className="sr-only">{msg('common.close', 'Close')}</span>
          </DialogClose>

          <DialogHeader className="px-[23px] pt-[19px] pr-14">
            <DialogTitle className="text-[22px] font-semibold leading-[26px] text-foreground">{title}</DialogTitle>
            <DialogDescription className="mt-1 text-sm leading-5 text-muted-foreground">
              {description}
            </DialogDescription>
          </DialogHeader>

          {children}
        </div>
      </DialogContent>
    </Dialog>
  );
}
