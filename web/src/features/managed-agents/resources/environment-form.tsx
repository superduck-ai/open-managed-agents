import { useEffect, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { DialogClose } from '../../../shared/ui/dialog';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../../shared/ui/alert-dialog';
import { navigateToInternalHref } from '../utils';
import { X } from 'lucide-react';
import { type ManagedEntityFormValues, type ManagedEntitySection } from '../types';

type UnsavedChangesGuardOptions = {
  blocked: boolean;
  dirty: boolean;
  onDiscard: () => void;
};

export function useUnsavedChangesGuard({ blocked, dirty, onDiscard }: UnsavedChangesGuardOptions) {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [pendingHref, setPendingHref] = useState<string | null>(null);
  const bypassRef = useRef(false);

  useEffect(() => {
    if (!dirty) {
      return;
    }
    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, [dirty]);

  useEffect(() => {
    if (!dirty) {
      return;
    }
    const handleDocumentClick = (event: MouseEvent) => {
      if (bypassRef.current || event.defaultPrevented || event.button !== 0) {
        return;
      }
      const target = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>('a[href]') : null;
      if (!target || target.target || target.download) {
        return;
      }
      const url = new URL(target.href, window.location.href);
      if (url.href === window.location.href) {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      if (!blocked) {
        setPendingHref(url.href);
        setConfirmOpen(true);
      }
    };
    document.addEventListener('click', handleDocumentClick, true);
    return () => document.removeEventListener('click', handleDocumentClick, true);
  }, [blocked, dirty]);

  const requestDiscard = () => {
    if (blocked) {
      return;
    }
    if (dirty) {
      setPendingHref(null);
      setConfirmOpen(true);
      return;
    }
    onDiscard();
  };

  const discard = () => {
    const href = pendingHref;
    bypassRef.current = true;
    setConfirmOpen(false);
    setPendingHref(null);
    onDiscard();
    if (!href) {
      return;
    }
    const url = new URL(href);
    if (url.origin === window.location.origin) {
      navigateToInternalHref(`${url.pathname}${url.search}${url.hash}`);
      return;
    }
    window.location.assign(href);
  };

  return {
    confirmOpen,
    requestDiscard,
    discard,
    continueEditing: () => {
      setConfirmOpen(false);
      setPendingHref(null);
    },
  };
}

export function UnsavedEnvironmentChangesDialog({
  open,
  onContinue,
  onDiscard,
}: {
  open: boolean;
  onContinue: () => void;
  onDiscard: () => void;
}) {
  const { msg } = useI18n();
  return (
    <AlertDialog open={open} onOpenChange={(nextOpen) => !nextOpen && onContinue()}>
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogTitle>
            {msg('managedAgents.environments.unsaved.title', 'Discard unsaved changes?')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {msg(
              'managedAgents.environments.unsaved.description',
              'Your changes have not been saved. Discard them and leave this form?',
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onContinue}>
            {msg('managedAgents.environments.unsaved.continueEditing', 'Continue editing')}
          </AlertDialogCancel>
          <AlertDialogAction type="button" variant="destructive" onClick={onDiscard}>
            {msg('managedAgents.environments.unsaved.discard', 'Discard changes')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export function useEnvironmentDialogDiscardGuard({
  section,
  values,
  initialValues,
  submitting,
  onClose,
}: {
  section: ManagedEntitySection;
  values: ManagedEntityFormValues;
  initialValues: ManagedEntityFormValues;
  submitting: boolean;
  onClose: () => void;
}) {
  const dirty =
    section === 'environments' &&
    (values.name.trim() !== initialValues.name.trim() || values.description !== initialValues.description);
  return useUnsavedChangesGuard({ blocked: submitting, dirty, onDiscard: onClose });
}

export function EnvironmentDialogCloseControl({
  environment,
  submitting,
  onRequestClose,
}: {
  environment: boolean;
  submitting: boolean;
  onRequestClose: () => void;
}) {
  const { msg } = useI18n();
  const button = (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={msg('common.close', 'Close')}
      disabled={environment && submitting}
      className="absolute right-0 top-0 text-foreground hover:bg-accent"
      onClick={environment ? onRequestClose : undefined}
    />
  );
  return environment ? (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      aria-label={msg('common.close', 'Close')}
      disabled={submitting}
      className="absolute right-0 top-0 text-foreground hover:bg-accent"
      onClick={onRequestClose}
    >
      <X className="size-[22px]" aria-hidden />
    </Button>
  ) : (
    <DialogClose render={button}>
      <X className="size-[22px]" aria-hidden />
    </DialogClose>
  );
}

export function EnvironmentDialogCancelControl({
  environment,
  submitting,
  onRequestClose,
}: {
  environment: boolean;
  submitting: boolean;
  onRequestClose: () => void;
}) {
  const { msg } = useI18n();
  return environment ? (
    <Button type="button" variant="outline" disabled={submitting} onClick={onRequestClose}>
      {msg('common.cancel', 'Cancel')}
    </Button>
  ) : (
    <DialogClose render={<Button type="button" variant="outline" />}>{msg('common.cancel', 'Cancel')}</DialogClose>
  );
}
