import { useId, useState } from 'react';
import { GitBranch, KeyRound } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/shared/ui/dialog';
import { Field, FieldLabel } from '@/shared/ui/field';
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
  const [token, setToken] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [updated, setUpdated] = useState<string | null>(null);
  const close = () => {
    setSelected(null);
    setToken('');
    setError(null);
  };
  return (
    <>
      <div className="space-y-2">
        {resources.map((resource, index) => {
          const checkout = objectRecord(resource.checkout);
          const ref = String(checkout.name ?? checkout.sha ?? '');
          return (
            <div key={resource.id ?? index} className="space-y-1 border-b border-border py-2 text-xs">
              <div className="flex min-w-0 items-center gap-2">
                <GitBranch className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                <span className="min-w-0 flex-1 break-all">{String(resource.url ?? '')}</span>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  disabled={archived || !resource.id}
                  aria-label={msg('managedAgents.git.updateToken', 'Update authorization token')}
                  onClick={() => {
                    setSelected(resource);
                    setToken('');
                    setError(null);
                  }}
                >
                  <KeyRound aria-hidden />
                </Button>
              </div>
              <p className="break-all font-mono text-muted-foreground">
                {resource.mount_path}
                {ref ? ` · ${checkout.type}: ${ref}` : ''}
              </p>
              {updated === resource.id ? (
                <p role="status">{msg('managedAgents.git.tokenUpdated', 'Authorization token updated.')}</p>
              ) : null}
            </div>
          );
        })}
      </div>
      <Dialog
        open={Boolean(selected)}
        onOpenChange={(open) => {
          if (!open && !saving) close();
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{msg('managedAgents.git.updateToken', 'Update authorization token')}</DialogTitle>
            <DialogDescription className="break-all">{String(selected?.url ?? '')}</DialogDescription>
          </DialogHeader>
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault();
              if (!selected?.id || saving || !token.trim()) return;
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
            <Field>
              <FieldLabel htmlFor={tokenId}>{msg('managedAgents.git.token', 'Authorization token')}</FieldLabel>
              <Input
                id={tokenId}
                type="password"
                autoComplete="new-password"
                required
                maxLength={8192}
                value={token}
                disabled={saving}
                onChange={(event) => setToken(event.target.value)}
              />
            </Field>
            {error ? (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            ) : null}
            <DialogFooter>
              <Button type="button" variant="outline" disabled={saving} onClick={close}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button type="submit" disabled={saving || !token.trim()}>
                {saving ? msg('common.saving', 'Saving...') : msg('common.save', 'Save')}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
