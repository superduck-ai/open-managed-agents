import type { MessageValues } from '../i18n/context';

type Translate = (id: string, defaultMessage: string, values?: MessageValues) => string;

export const roleOptions = [
  { label: 'User', value: 'user', description: 'Use Workbench, development resources and workspace API keys' },
  { label: 'Admin', value: 'admin', description: 'Manage members, organization, workspaces and billing' },
] as const;

export type PlatformRole = (typeof roleOptions)[number]['value'];

const roleCopy = {
  user: {
    labelId: 'members.role.user',
    label: roleOptions[0].label,
    descriptionId: 'members.role.user.description',
    description: roleOptions[0].description,
  },
  admin: {
    labelId: 'members.role.admin',
    label: roleOptions[1].label,
    descriptionId: 'members.role.admin.description',
    description: roleOptions[1].description,
  },
};

export function platformRoleLabel(role: PlatformRole, msg: Translate) {
  const copy = roleCopy[role];
  return msg(copy.labelId, copy.label);
}

export function platformRoleDescription(role: PlatformRole, msg: Translate) {
  const copy = roleCopy[role];
  return msg(copy.descriptionId, copy.description);
}

export function accountRoleLabel(role: string | undefined, msg: Translate) {
  return platformRoleLabel(role === 'admin' || role === 'workspace_admin' ? 'admin' : 'user', msg);
}

export function membershipRoleForOrganization(
  memberships: readonly { role?: string; organization?: { uuid?: string } }[] | undefined,
  organizationUuid: string | undefined,
) {
  const matchedOrganizationUuid =
    organizationUuid ?? memberships?.find((membership) => membership.organization?.uuid)?.organization?.uuid;
  if (!matchedOrganizationUuid) {
    return undefined;
  }
  return memberships?.find((membership) => membership.organization?.uuid === matchedOrganizationUuid)?.role;
}
