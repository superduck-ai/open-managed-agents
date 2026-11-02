import type { PlatformAccess } from '../../shared/auth/api';

export type PlatformBootstrap = {
  account: { uuid: string; name: string; email: string };
  platform_access: PlatformAccess;
  csrf_token: string;
};
export type Organization = {
  uuid: string;
  name: string;
  website: string;
  industry: string;
  version: number;
  member_count: number;
  workspace_count: number;
  admin_count: number;
  created_at: string;
};
export type Page<T> = { data: T[]; next_cursor: string };
