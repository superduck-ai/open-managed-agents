export type SystemSkill = {
  bundled?: boolean;
  bundled_version?: string | null;
  icon_key?: string;
  id: string;
  display_title: string;
  default_version: string | null;
  listed: boolean;
  revision: number;
  created_at: string;
  updated_at: string;
};
export type SystemVersion = {
  id: string;
  version: string;
  name: string;
  description: string;
  state: 'draft' | 'published';
  size_bytes: number;
  sha256: string;
  created_at: string;
};
export type SystemEvent = {
  id: string;
  actor_uuid: string;
  actor_id?: string;
  actor_name?: string;
  action: string;
  version: string;
  created_at: string;
};
export type SystemPage<T> = { data: T[]; has_more: boolean };
export type SystemPackage = {
  name: string;
  description: string;
  sha256: string;
  size_bytes: number;
  files: string[];
  skill_md: string;
  existing: SystemSkill | null;
};
export type SystemAction = 'set_default' | 'list' | 'unlist';
export const systemSkillsKey = ['platform-admin', 'system-skills'] as const;
export const systemSkillPageSize = 20;
