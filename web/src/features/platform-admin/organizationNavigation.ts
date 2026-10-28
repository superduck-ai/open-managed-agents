import { Box, Mail, Settings, UsersRound } from 'lucide-react';

export const organizationSections = [
  { value: 'profile', labelId: 'platformAdmin.organizationDetails', label: 'Organization details', icon: Settings },
  { value: 'members', labelId: 'platformAdmin.organizationMembers', label: 'Organization members', icon: UsersRound },
  { value: 'workspaces', labelId: 'platformAdmin.workspaces', label: 'Workspaces', icon: Box },
  { value: 'invitations', labelId: 'platformAdmin.memberInvitations', label: 'Member invitations', icon: Mail },
] as const;

export type OrganizationSection = (typeof organizationSections)[number]['value'];

export function organizationSection(value: unknown): OrganizationSection {
  return organizationSections.find((section) => section.value === value)?.value ?? 'profile';
}
