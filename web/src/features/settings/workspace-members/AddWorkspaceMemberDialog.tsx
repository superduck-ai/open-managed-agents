import { useQuery } from '@tanstack/react-query';
import { useI18n } from '../../../shared/i18n';
import { useState } from 'react';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from '../../../shared/ui/dialog';
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
  open,
  onClose,
  onAdd,
  pending,
  error,
}: {
  orgUuid: string;
  workspaceId: string;
  workspaceName: string;
  open: boolean;
  onClose: () => void;
  onAdd: (userId: string, role: WorkspaceMemberRole) => void;
  pending: boolean;
  error?: string;
}) {
  const { msg } = useI18n();
  const [userId, setUserId] = useState('');
  const [role, setRole] = useState<WorkspaceMemberRole>('workspace_developer');
  const candidates = useQuery({
    queryKey: ['console', 'workspace-member-candidates', orgUuid, workspaceId],
    queryFn: () => listWorkspaceMemberCandidates(orgUuid, workspaceId),
    enabled: open,
    retry: false,
  });
  const members = candidates.data ?? [];
  const selected = candidates.data?.find((candidate) => candidate.user_id === userId);
  const failure = error ?? candidates.error?.message;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onClose();
      }}
    >
      <DialogContent className="gap-4 p-6 sm:max-w-[540px]" aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle className="pr-6 text-2xl leading-tight">
            {msg('members.addDialog.title', 'Add member to {workspaceName}', { workspaceName })}
          </DialogTitle>
        </DialogHeader>
        <div className="grid gap-2">
          <Label htmlFor="workspace-member">{msg('members.addDialog.member', 'Member')}</Label>
          <Select
            value={userId}
            onValueChange={(value) => setUserId(value ?? '')}
            disabled={pending || candidates.isLoading}
          >
            <SelectTrigger id="workspace-member" className="w-full bg-muted/50">
              <SelectValue placeholder={msg('members.addDialog.select', 'Select')}>
                {selected ? memberLabel(selected) : undefined}
              </SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false} sideOffset={6} className="rounded-xl">
              {members.map((candidate) => (
                <SelectItem
                  key={candidate.user_id}
                  value={candidate.user_id}
                  className="min-h-10 rounded-lg py-2 pl-4 pr-10 [&>span:first-child]:min-w-0 [&>span:first-child]:shrink [&>span:first-child]:overflow-hidden [&>span:last-child]:right-4 [&>span:last-child]:text-blue-400"
                >
                  <span className="truncate">{memberLabel(candidate)}</span>
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
        <div className="grid gap-2">
          <Label htmlFor="workspace-member-role">{msg('members.addDialog.role', 'Role')}</Label>
          <MemberRoleSelect
            value={role}
            onChange={setRole}
            disabled={pending}
            label={msg('members.addDialog.role', 'Role')}
            id="workspace-member-role"
            className="w-full bg-muted/50"
          />
        </div>
        {failure ? (
          <Alert variant="destructive">
            <AlertDescription>{failure}</AlertDescription>
          </Alert>
        ) : null}
        <DialogFooter>
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
