import { useEffect, useId, useRef, useState } from 'react';
import { GitBranch, KeyRound } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/shared/ui/dialog';
import { Field, FieldDescription, FieldLabel } from '@/shared/ui/field';
import { Input } from '@/shared/ui/input';
import { updateSessionGitResourceToken } from '../api';
import type { SessionResourceApiResponse } from '../types';
import { errorMessage, objectRecord } from '../utils';

export function SessionGitResources({
  resources,
  sessionId,
  workspaceId,
  archived,
}: {
  resources: SessionResourceApiResponse[];
  sessionId: string;
  workspaceId: string;
  archived: boolean;
}) {
  const { msg } = useI18n();
  const tokenId = useId();
  const [selected, setSelected] = useState<SessionResourceApiResponse | null>(null);
  const [open, setOpen] = useState(false);
  const tokenRef = useRef<HTMLInputElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const errorRef = useRef<HTMLParagraphElement>(null);
  const [token, setToken] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [updated, setUpdated] = useState<string | null>(null);
  const close = () => setOpen(false);
  useEffect(() => {
    if (error) errorRef.current?.focus();
  }, [error]);
  return (
    <>
      <div className="space-y-2">
        {resources.map((resource, index) => {
          const checkout = objectRecord(resource.checkout);
          const ref = String(checkout.name ?? checkout.sha ?? '');
          return (
            <div key={resource.id ?? index} className="space-y-1 border-b-[0.5px] border-border py-2 text-xs">
              <div className="flex min-w-0 items-center gap-2">
                <GitBranch className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                <span className="min-w-0 flex-1 break-all">{String(resource.url ?? '')}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  disabled={archived || !resource.id}
                  aria-label={msg('managedAgents.git.updateToken', 'Update authorization token')}
                  className="shrink-0"
                  onClick={(event) => {
                    triggerRef.current = event.currentTarget;
                    setSelected(resource);
                    setOpen(true);
                    setToken('');
                    setError(null);
                  }}
                >
                  <KeyRound aria-hidden />
                </Button>
              </div>
              <div className="flex flex-wrap gap-x-3 gap-y-1 font-mono text-muted-foreground">
                <p className="break-all">{resource.mount_path}</p>
                {ref ? <p className="break-all">{`${checkout.type}: ${ref}`}</p> : null}
              </div>
              {updated === resource.id ? (
                <p role="status">{msg('managedAgents.git.tokenUpdated', 'Authorization token updated.')}</p>
              ) : null}
            </div>
          );
        })}
      </div>
      <Dialog
        open={open}
        onOpenChange={(nextOpen, details) => {
          if (!nextOpen && saving) {
            details.cancel();
            return;
          }
          setOpen(nextOpen);
        }}
        onOpenChangeComplete={(nextOpen) => {
          if (!nextOpen) {
            setSelected(null);
            setToken('');
            setError(null);
          }
        }}
      >
        <DialogContent
          initialFocus={tokenRef}
          finalFocus={triggerRef}
          closeButtonDisabled={saving}
          className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-md"
        >
          <DialogHeader className="shrink-0">
            <DialogTitle>{msg('managedAgents.git.updateToken', 'Update authorization token')}</DialogTitle>
            <DialogDescription className="line-clamp-2 break-all" title={String(selected?.url ?? '')}>
              {String(selected?.url ?? '')}
            </DialogDescription>
          </DialogHeader>
          <form
            className="flex min-h-0 flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (!selected?.id || saving) return;
              setSaving(true);
              setError(null);
              void updateSessionGitResourceToken(sessionId, selected.id, token, workspaceId)
                .then(() => {
                  setUpdated(selected.id!);
                  close();
                })
                .catch((failure) => setError(errorMessage(failure)))
                .finally(() => setSaving(false));
            }}
          >
            <div className="subtle-scrollbar min-h-0 flex-1 space-y-4 overflow-y-auto px-px">
              <Field>
                <FieldLabel htmlFor={tokenId}>{msg('managedAgents.git.token', 'Authorization token')}</FieldLabel>
                <Input
                  ref={tokenRef}
                  id={tokenId}
                  aria-describedby={`${tokenId}-help`}
                  type="password"
                  autoComplete="new-password"
                  maxLength={8192}
                  value={token}
                  disabled={saving}
                  onChange={(event) => setToken(event.target.value)}
                />
                <FieldDescription id={`${tokenId}-help`}>
                  {msg('managedAgents.git.clearTokenHelp', 'Leave blank to remove the token and use anonymous access.')}
                </FieldDescription>
              </Field>
              {error ? (
                <p
                  ref={errorRef}
                  role="alert"
                  tabIndex={-1}
                  className="text-sm text-destructive outline-none [overflow-wrap:anywhere]"
                >
                  {error}
                </p>
              ) : null}
            </div>
            <DialogFooter className="shrink-0">
              <Button type="button" variant="outline" disabled={saving} onClick={close}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button type="submit" disabled={saving}>
                {saving ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
