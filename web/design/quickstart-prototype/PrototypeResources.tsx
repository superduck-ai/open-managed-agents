import { z } from 'zod';
export const resourcesSchema = z.object({
  skills: z.array(z.enum(['spreadsheet', 'documents'])).default([]),
  vault: z.enum(['none', 'integrations']).default('none'),
  credentialReady: z.boolean().default(false),
  credentialSkipped: z.boolean().default(false),
  slackEnabled: z.boolean().default(false),
});
export type PrototypeResources = z.infer<typeof resourcesSchema>;
export const emptyResources: PrototypeResources = {
  skills: [],
  vault: 'none',
  credentialReady: false,
  credentialSkipped: false,
  slackEnabled: false,
};
export const skillOptions = [
  { value: 'spreadsheet', label: '表格分析', version: '1' },
  { value: 'documents', label: '文档处理', version: '2' },
] as const;

export function skillReferences(resources: PrototypeResources) {
  return skillOptions
    .filter((skill) => resources.skills.includes(skill.value))
    .map((skill) => ({ type: 'custom', skill_id: `SKILL_${skill.value.toUpperCase()}_ID`, version: skill.version }));
}

export function agentResourceFields(resources: PrototypeResources) {
  return {
    ...(resources.skills.length > 0 ? { skills: skillReferences(resources) } : {}),
    mcp_servers: resources.slackEnabled ? [{ name: 'slack', type: 'url', url: 'https://mcp.slack.com/mcp' }] : [],
    tools: [
      { type: 'agent_toolset_20260401' },
      ...(resources.slackEnabled
        ? [
            {
              type: 'mcp_toolset',
              mcp_server_name: 'slack',
              default_config: { permission_policy: { type: 'always_ask' } },
            },
          ]
        : []),
    ],
  };
}

export function needsCredential(resources: PrototypeResources) {
  return (
    resources.slackEnabled && !resources.credentialSkipped && (resources.vault === 'none' || !resources.credentialReady)
  );
}

export function vaultReferences(resources: PrototypeResources) {
  return resources.slackEnabled && resources.vault !== 'none' ? ['VAULT_ID'] : [];
}
