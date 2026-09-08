import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '../../../shared/ui/dialog';
import { Button } from '../../../shared/ui/button';
import { Input } from '../../../shared/ui/input';
import { Label } from '../../../shared/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { listWorkspaceMemberCandidates, type WorkspaceMemberRole } from './api';
import { MemberRoleSelect } from './MemberRoleSelect';

export function AddWorkspaceMemberDialog({
  orgUuid,
  workspaceId,
  open,
  onClose,
  onAdd,
  pending,
  error,
}: {
  orgUuid: string;
  workspaceId: string;
  open: boolean;
  onClose: () => void;
  onAdd: (userId: string, role: WorkspaceMemberRole) => void;
  pending: boolean;
  error?: string;
}) {
  const [search, setSearch] = useState('');
  const [userId, setUserId] = useState('');
  const [role, setRole] = useState<WorkspaceMemberRole>('workspace_user');
  const candidates = useQuery({
    queryKey: ['console', 'workspace-member-candidates', orgUuid, workspaceId],
    queryFn: () => listWorkspaceMemberCandidates(orgUuid, workspaceId),
    enabled: open,
    retry: false,
  });
  const filtered =
    candidates.data?.filter((candidate) =>
      `${candidate.name} ${candidate.email}`.toLowerCase().includes(search.toLowerCase()),
    ) ?? [];
  const selected = candidates.data?.find((candidate) => candidate.user_id === userId);
  const failure = error ?? candidates.error?.message;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !pending) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add to Workspace</DialogTitle>
          <DialogDescription>Select an active organization member and assign a workspace role.</DialogDescription>
        </DialogHeader>
        <Input
          aria-label="Search organization members"
          placeholder="Search by name or email"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Label>Organization member</Label>
        <Select
          value={userId}
          onValueChange={(value) => setUserId(value ?? '')}
          disabled={pending || candidates.isLoading}
        >
          <SelectTrigger aria-label="Organization member">
            <SelectValue placeholder="Select a member">
              {selected ? `${selected.name} (${selected.email})` : undefined}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {filtered.map((candidate) => (
              <SelectItem key={candidate.user_id} value={candidate.user_id}>
                {candidate.name || candidate.email} — {candidate.email}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {!candidates.isLoading && filtered.length === 0 ? (
          <p className="text-sm text-muted-foreground">No eligible organization members found.</p>
        ) : null}
        <Label>Workspace role</Label>
        <MemberRoleSelect value={role} onChange={setRole} disabled={pending} label="Workspace role" />
        {failure ? (
          <Alert variant="destructive">
            <AlertDescription>{failure}</AlertDescription>
          </Alert>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={pending}>
            Cancel
          </Button>
          <Button disabled={!selected || pending} onClick={() => onAdd(userId, role)}>
            {pending ? 'Adding...' : 'Add member'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
