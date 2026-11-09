import { type ComponentProps, useState } from 'react';
import type { Dialog } from '../ui/dialog';

type DialogDismissalProps = Pick<ComponentProps<typeof Dialog>, 'open' | 'onOpenChange' | 'onOpenChangeComplete'>;

export function useDialogDismissal(onClose: () => void, disabled = false): DialogDismissalProps {
  const [open, setOpen] = useState(true);
  return {
    open,
    onOpenChange: (nextOpen, details) => {
      if (!nextOpen && disabled) {
        details.cancel();
        return;
      }
      setOpen(nextOpen);
    },
    onOpenChangeComplete: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  };
}
