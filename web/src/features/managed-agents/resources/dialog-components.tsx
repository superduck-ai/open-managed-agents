import { X } from 'lucide-react';
import { cloneElement } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { DialogClose, DialogDescription, DialogTitle } from '../../../shared/ui/dialog';
import { submitLabel } from '../labels';
import { type ManagedEntitySection } from '../types';

export function ManagedDialogHeader({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className="pr-8">
      <DialogTitle className="text-[22px] font-semibold leading-[26px] text-foreground">{title}</DialogTitle>
      {subtitle ? (
        <DialogDescription className="mt-1 text-sm leading-5 text-muted-foreground">{subtitle}</DialogDescription>
      ) : null}
    </div>
  );
}

export function ManagedDialogCloseControl({ disabled, onClick }: { disabled?: boolean; onClick?: () => void }) {
  const { msg } = useI18n();
  const button = (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={msg('common.close', 'Close')}
      disabled={disabled}
      className="absolute right-0 top-0 text-foreground hover:bg-accent"
      onClick={onClick}
    />
  );
  const icon = <X className="size-[22px]" aria-hidden />;
  return onClick ? cloneElement(button, undefined, icon) : <DialogClose render={button}>{icon}</DialogClose>;
}

export function ManagedEntityDialogActions({
  section,
  editing,
  submitting,
  canSubmit,
}: {
  section: ManagedEntitySection;
  editing: boolean;
  submitting: boolean;
  canSubmit: boolean;
}) {
  const { msg } = useI18n();
  return (
    <div className="mt-5 flex justify-end gap-2">
      <DialogClose render={<Button type="button" variant="outline" />}>{msg('common.cancel', 'Cancel')}</DialogClose>
      <Button type="submit" disabled={!canSubmit}>
        {submitting ? msg('common.saving', 'Saving...') : submitLabel(section, editing, msg)}
      </Button>
    </div>
  );
}
