import type { MessageValues } from '../i18n/context';

type Translate = (id: string, defaultMessage: string, values?: MessageValues) => string;

export const roleOptions = [
  {
    label: 'User',
    value: 'user',
    description: 'Use Workbench',
  },
  {
    label: 'Claude Code',
    value: 'claude_code_user',
    description: 'Use Workbench and Claude Code',
  },
  {
    label: 'Developer',
    value: 'developer',
    description: 'Use Workbench, Claude Code and manage API keys',
  },
  {
    label: 'Billing',
    value: 'billing',
    description: 'Use Workbench and manage billing details',
  },
  {
    label: 'Admin',
    value: 'admin',
    description: 'Do all of the above, plus manage users',
  },
] as const;

export type PlatformRole = (typeof roleOptions)[number]['value'];

const roleCopy = {
  user: {
    labelId: 'members.role.user',
    label: 'User',
    descriptionId: 'members.role.user.description',
    description: 'Use Workbench',
  },
  claude_code_user: {
    labelId: 'members.role.claudeCode',
    label: 'Claude Code',
    descriptionId: 'members.role.claudeCode.description',
    description: 'Use Workbench and Claude Code',
  },
  developer: {
    labelId: 'members.role.developer',
    label: 'Developer',
    descriptionId: 'members.role.developer.description',
    description: 'Use Workbench, Claude Code and manage API keys',
  },
  billing: {
    labelId: 'members.role.billing',
    label: 'Billing',
    descriptionId: 'members.role.billing.description',
    description: 'Use Workbench and manage billing details',
  },
  admin: {
    labelId: 'members.role.admin',
    label: 'Admin',
    descriptionId: 'members.role.admin.description',
    description: 'Do all of the above, plus manage users',
  },
} satisfies Record<PlatformRole, { labelId: string; label: string; descriptionId: string; description: string }>;

export function platformRoleLabel(role: PlatformRole, msg: Translate) {
  const copy = roleCopy[role];
  return msg(copy.labelId, copy.label);
}

export function platformRoleDescription(role: PlatformRole, msg: Translate) {
  const copy = roleCopy[role];
  return msg(copy.descriptionId, copy.description);
}

export function accountRoleLabel(role: string | undefined, msg: Translate) {
  if (!role) {
    return platformRoleLabel('admin', msg);
  }
  if (isPlatformRole(role)) {
    return platformRoleLabel(role, msg);
  }
  return titleizeRole(role);
}

function isPlatformRole(role: string): role is PlatformRole {
  return Object.hasOwn(roleCopy, role);
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

function titleizeRole(role: string) {
  return role
    .split('_')
    .filter(Boolean)
    .map((part) => `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`)
    .join(' ');
}
