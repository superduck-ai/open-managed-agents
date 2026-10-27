import { useQuery } from '@tanstack/react-query';
import { useI18n } from '../../../shared/i18n';
import { type ComponentProps, useEffect, useState } from 'react';
import { Dialog, DialogClose, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '../../../shared/ui/dialog';
import { Button } from '../../../shared/ui/button';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { listWorkspaceMemberCandidates, type WorkspaceMemberRole } from './api';
import { MemberRoleSelect } from './MemberRoleSelect';

export function AddWorkspaceMemberDialog({
  orgUuid,
  workspaceId,
  workspaceName,
  returnFocus,
  open,
  onClose,
  onAdd,
  pending,
  error,
}: {
  orgUuid: string;
  workspaceId: string;
  workspaceName: string;
  returnFocus?: ComponentProps<typeof DialogContent>['finalFocus'];
  open: boolean;
  onClose: () => void;
  onAdd: (userId: string, role: WorkspaceMemberRole) => void;
  pending: boolean;
  error?: string;
}) {
  const { msg } = useI18n();
  const [dialogOpen, setDialogOpen] = useState(open);
  useEffect(() => setDialogOpen(open), [open]);
  const [userId, setUserId] = useState('');
  const [role, setRole] = useState<WorkspaceMemberRole>('workspace_user');
  const candidates = useQuery({
    queryKey: ['console', 'workspace-member-candidates', orgUuid, workspaceId],
    queryFn: () => listWorkspaceMemberCandidates(orgUuid, workspaceId),
    enabled: open,
    retry: false,
  });
  const members = candidates.data ?? [];
  const unavailable = candidates.isError || members.length === 0;
  const selected = candidates.data?.find((candidate) => candidate.user_id === userId);
  const failure = error ?? candidates.error?.message;
  const title = msg('members.addDialog.title', 'Add member to {workspaceName}', { workspaceName });
  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={(nextOpen, details) => {
        if (!nextOpen && pending) {
          details.cancel();
          return;
        }
        setDialogOpen(nextOpen);
      }}
      onOpenChangeComplete={(nextOpen) => {
        if (!nextOpen) onClose();
      }}
    >
      <DialogContent
        closeButtonDisabled={pending}
        finalFocus={returnFocus}
        className="grid-rows-[auto_minmax(0,1fr)] max-h-[calc(100dvh-2rem)] gap-0 overflow-clip p-0 sm:max-w-[540px]"
        aria-describedby={undefined}
      >
        <DialogHeader className="min-w-0 px-6 pt-6 pb-4">
          <DialogTitle className="line-clamp-2 break-words pr-6 text-xl leading-7" title={title}>
            {title}
          </DialogTitle>
        </DialogHeader>
        <div className="subtle-scrollbar min-h-0 min-w-0 space-y-4 overflow-y-auto px-6">
          <div className="grid min-w-0 gap-2">
            <Label htmlFor="workspace-member">{msg('members.addDialog.member', 'Member')}</Label>
            <Select
              value={userId || null}
              onValueChange={(value) => setUserId(value ?? '')}
              disabled={pending || candidates.isLoading || unavailable}
            >
              <SelectTrigger id="workspace-member" className="w-full">
                <SelectValue placeholder={msg('members.addDialog.select', 'Select')}>
                  {selected ? memberLabel(selected) : undefined}
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {members.map((candidate) => (
                  <SelectItem
                    key={candidate.user_id}
                    value={candidate.user_id}
                    label={memberLabel(candidate)}
                    aria-label={memberLabel(candidate)}
                  >
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate">{candidate.name || candidate.email}</span>
                      {candidate.name ? (
                        <span className="truncate text-xs text-muted-foreground">{candidate.email}</span>
                      ) : null}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {!candidates.isLoading && !candidates.isError && members.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {msg('members.addDialog.noEligible', 'No eligible organization members found.')}
              </p>
            ) : null}
          </div>
          <div className="grid min-w-0 gap-2">
            <Label htmlFor="workspace-member-role">{msg('members.addDialog.role', 'Role')}</Label>
            <MemberRoleSelect
              value={role}
              onChange={setRole}
              disabled={pending || unavailable}
              label={msg('members.addDialog.role', 'Role')}
              id="workspace-member-role"
              className="w-full"
            />
          </div>
          {failure ? (
            <Alert variant="destructive">
              <AlertDescription>{failure}</AlertDescription>
            </Alert>
          ) : null}
        </div>
        <DialogFooter className="px-6 pt-4 pb-6">
          <DialogClose disabled={pending} render={<Button type="button" variant="outline" />}>
            {msg('common.cancel', 'Cancel')}
          </DialogClose>
          <Button disabled={!selected || pending} onClick={() => onAdd(userId, role)}>
            {pending ? msg('members.addDialog.adding', 'Adding...') : msg('members.addToWorkspace', 'Add to Workspace')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function memberLabel(member: { name: string; email: string }) {
  return member.name ? `${member.name} (${member.email})` : member.email;
}
