import { useState } from 'react';
import { hasScopeUnsavedChanges } from '../../shared/organizations/unsaved';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '../../shared/ui/alert-dialog';

export function useScopeConfirmation() {
  const [pending, setPending] = useState<(() => void) | null>(null);
  const request = (action: () => void) => {
    if (hasScopeUnsavedChanges()) setPending(() => action);
    else action();
  };
  const dialog = pending ? (
    <AlertDialog
      open={Boolean(pending)}
      onOpenChange={(open) => {
        if (!open) setPending(null);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>放弃未保存的更改？</AlertDialogTitle>
          <AlertDialogDescription>切换组织或工作区会关闭当前页面，未保存的更改将丢失。</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={() => setPending(null)}>继续编辑</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              const action = pending;
              setPending(null);
              action?.();
            }}
          >
            放弃更改并切换
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  ) : null;
  return { request, dialog };
}
