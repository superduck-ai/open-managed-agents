export type SystemSkill = {
  bundled?: boolean;
  bundled_version?: string | null;
  icon_key?: string;
  id: string;
  display_title: string;
  default_version: string | null;
  listed: boolean;
  created_at: string;
  updated_at: string;
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
export type SystemAction = 'list' | 'unlist';
export const systemSkillsKey = ['platform-admin', 'system-skills'] as const;
export const systemSkillPageSize = 20;
