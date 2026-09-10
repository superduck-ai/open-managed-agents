import { useMutation, useQueryClient } from '@tanstack/react-query';
import { MoreVertical, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { MemberRemovalDialog } from './MemberRemovalDialog';
import { Button } from '../../shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { toast } from '../../shared/ui/sonner';
import { removeOrganizationMember, type OrganizationMember } from './membersApi';

export function OrganizationMemberRemoval({
  orgUuid,
  organizationName,
  member,
  csrfToken,
  disabled,
}: {
  orgUuid: string;
  organizationName: string;
  member: OrganizationMember;
  csrfToken?: string;
  disabled?: boolean;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const queryClient = useQueryClient();
  const removal = useMutation({
    mutationFn: () => removeOrganizationMember(orgUuid, member.id, csrfToken),
    onSuccess: async () => {
      setOpen(false);
      await queryClient.invalidateQueries({ queryKey: ['console', 'organization-members', orgUuid] });
      toast.success(msg('members.removedToast', 'Member removed.'));
    },
  });

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          disabled={disabled}
          render={
            <Button
              variant="ghost"
              size="icon"
              aria-label={msg('members.moreActionsFor', `More actions for ${member.name || member.email}`, {
                name: member.name || member.email,
              })}
            />
          }
        >
          <MoreVertical className="size-4" aria-hidden />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem
            variant="destructive"
            onClick={() => {
              removal.reset();
              setOpen(true);
            }}
          >
            <Trash2 className="size-4" aria-hidden />
            {msg('members.removeMember', 'Remove member')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <MemberRemovalDialog
        open={open}
        title={msg('members.orgRemoveDialogTitle', 'Remove member?')}
        pending={removal.isPending}
        error={removal.error?.message}
        onClose={() => setOpen(false)}
        onConfirm={() => removal.mutate()}
      >
        {msg(
          'members.orgRemoveDialogBody',
          'Remove {email} from {organization}? They will lose access to this organization and all its workspaces. Workspace API keys, credential vaults, resources and team sessions will remain. Their membership in other organizations will not change.',
          { email: member.email, organization: organizationName },
        )}
      </MemberRemovalDialog>
    </>
  );
}
