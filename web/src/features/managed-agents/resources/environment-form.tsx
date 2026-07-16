import { useBlocker } from '@tanstack/react-router';
import { useEffect, useRef, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
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
import { type ManagedEntityFormValues } from '../types';
import { ManagedDialogSubmitButton } from './dialog-components';

type UnsavedChangesGuardOptions = {
  dirty: boolean;
  interactionBlocked: boolean;
  onDiscard: () => void;
};

export function useUnsavedChangesGuard({ dirty, interactionBlocked, onDiscard }: UnsavedChangesGuardOptions) {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [pendingHref, setPendingHref] = useState<string | null>(null);
  const bypassRef = useRef(false);
  const navigationBlocker = useBlocker({
    shouldBlockFn: () => dirty && !bypassRef.current,
    enableBeforeUnload: dirty,
    disabled: !dirty,
    withResolver: true,
  });

  useEffect(() => {
    if (navigationBlocker.status !== 'blocked' || interactionBlocked) {
      return;
    }
    setPendingHref(null);
    setConfirmOpen(true);
  }, [interactionBlocked, navigationBlocker.status]);

  useEffect(() => {
    if (!dirty) {
      return;
    }
    const handleDocumentClick = (event: MouseEvent) => {
      if (
        bypassRef.current ||
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey
      ) {
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
      if (!interactionBlocked) {
        setPendingHref(url.href);
        setConfirmOpen(true);
      }
    };
    document.addEventListener('click', handleDocumentClick, true);
    return () => document.removeEventListener('click', handleDocumentClick, true);
  }, [dirty, interactionBlocked]);

  const requestDiscard = () => {
    if (interactionBlocked) {
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
    if (navigationBlocker.status === 'blocked') {
      navigationBlocker.proceed();
      onDiscard();
      return;
    }
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
      if (navigationBlocker.status === 'blocked') {
        navigationBlocker.reset();
      }
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
  values,
  initialValues,
  submitting,
  onClose,
}: {
  values: ManagedEntityFormValues;
  initialValues: ManagedEntityFormValues;
  submitting: boolean;
  onClose: () => void;
}) {
  const dirty = environmentDialogFingerprint(values) !== environmentDialogFingerprint(initialValues);
  return useUnsavedChangesGuard({ dirty, interactionBlocked: submitting, onDiscard: onClose });
}

function environmentDialogFingerprint(values: ManagedEntityFormValues) {
  return JSON.stringify({ name: values.name.trim(), description: values.description.trim() });
}

export function EnvironmentDialogActions({
  editing,
  submitting,
  canSubmit,
  onRequestClose,
}: {
  editing: boolean;
  submitting: boolean;
  canSubmit: boolean;
  onRequestClose: () => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="mt-5 flex justify-end gap-2">
      <Button type="button" variant="outline" disabled={submitting} onClick={onRequestClose}>
        {msg('common.cancel', 'Cancel')}
      </Button>
      <ManagedDialogSubmitButton
        section="environments"
        editing={editing}
        submitting={submitting}
        canSubmit={canSubmit}
      />
    </div>
  );
}
