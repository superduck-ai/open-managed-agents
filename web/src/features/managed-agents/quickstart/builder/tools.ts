import { environmentPackagesSchema, environmentPackagesMetadataKey } from './environmentRequirements';
import { builderFileDeliverySystem } from './fileDelivery';
import { z } from 'zod';
import { createAgentDraftSchema, normalizeCreateAgentDraft } from '../../agents/create-dialog-model';
import { quickstartBuildAgentConfigInput } from '../../agentConfig';
import { platformQuickstartTools } from '../platformQuickstartRequest';
import { previewSchedule } from '../../resources/deployment-schedule';
import type { BuilderState, BuilderTool } from './model';

const configurationDescription =
  'Propose the complete agent configuration for user confirmation before saving. Include user-supplied source material needed by future sessions in system as reference data, preserving facts and unknowns. Keep explicit user restrictions on runtime behavior in their own wording in a User constraints section of system; do not replace them with weaker optional or unconfirmed exceptions. Declare software the user requests for runtime setup in environment_packages by package manager, including on the initial proposal; do not claim it is installed in system. Preserve declared dependencies on partial updates. File deliverables must use the platform outputs mount and a downloadable link, never a working-directory-only path. When narrowing output scope, replace the complete output contract and remove obsolete sections while preserving unrelated fields. Preserve explicit JSON field types, missing-value handling and JSON-only format; clarify an unspecified type if choosing a representation changes correctness. This call opens a confirmation card; it does not mean the agent is saved. Only a successful save result confirms persistence.';
const empty = z.object({}).strict();
const question = z
  .object({
    id: z.string().optional(),
    header: z.string().min(1),
    question: z.string().min(1),
    multiSelect: z.boolean().optional(),
    multi_select: z.boolean().optional(),
    options: z
      .array(z.object({ label: z.string().min(1), description: z.string().optional() }).strict())
      .min(2)
      .max(4),
  })
  .strict();
const networking = z.discriminatedUnion('type', [
  z.object({ type: z.literal('unrestricted') }).strict(),
  z
    .object({
      type: z.literal('limited'),
      allow_mcp_servers: z.boolean().default(false),
      allow_package_managers: z.boolean().default(false),
      allowed_hosts: z.array(z.string().min(1)).max(100).default([]),
    })
    .strict(),
]);
export const environmentInputSchema = z
  .object({
    reuse_environment_id: z.string().min(1).optional(),
    name: z.string().trim().min(1).optional(),
    description: z.string().optional(),
    config: z
      .object({ type: z.literal('cloud'), networking, packages: environmentPackagesSchema.optional() })
      .strict()
      .optional(),
  })
  .strict()
  .refine(
    (value) => (value.reuse_environment_id ? !value.config && !value.name : Boolean(value.name && value.config)),
    'Choose an existing environment or provide a name and config.',
  );
export const deploymentInputSchema = z
  .object({
    name: z.string().trim().min(1),
    cron_expression: z.string().trim().min(1),
    timezone: z.string().optional(),
    initial_message: z.string().trim().min(1),
  })
  .strict();
export const questionInputSchema = z.object({ questions: z.array(question).min(1).max(4) }).strict();
const configInput = z
  .object(createAgentDraftSchema.shape)
  .partial()
  .extend({ environment_packages: environmentPackagesSchema.optional() })
  .strict();
export const builderToolInputs = {
  ask_user_questions: questionInputSchema,
  build_agent_config: configInput,
  list_environments: empty,
  pick_environment: empty,
  create_environment: environmentInputSchema,
  list_vaults: empty,
  pick_vault: z.object({ recommended: z.array(z.string()).max(2).optional() }).strict(),
  select_vault: z.object({ vault_ids: z.array(z.string().min(1)).min(1).max(8) }).strict(),
  create_vault: z.object({ name: z.string().trim().min(1) }).strict(),
  create_vault_credential: z
    .object({ vault_id: z.string().min(1), mcp_server_name: z.string().min(1), reason: z.string().optional() })
    .strict(),
  agent_ready: z.object({ suggested_first_message: z.string().min(1) }).strict(),
  await_test_run: z.object({ until: z.enum(['first_message', 'session_closed']) }).strict(),
  offer_next_step: empty,
  show_integration_exits: z.object({ agent_id: z.string().optional(), environment_id: z.string().optional() }).strict(),
  flag_schedule_intent: z.object({ wants_schedule: z.boolean() }).strict(),
  create_deployment: deploymentInputSchema,
};
export function validateBuilderTool(tool: BuilderTool) {
  const schema = builderToolInputs[tool.name as keyof typeof builderToolInputs];
  if (!schema) throw new Error(`Unsupported builder tool: ${tool.name}`);
  return schema.parse(tool.input);
}
export function proposedAgentConfig(tool: BuilderTool, state: BuilderState, models: string[]) {
  configInput.parse(tool.input);
  const config = normalizeCreateAgentDraft(
    createAgentDraftSchema.parse(quickstartBuildAgentConfigInput(tool.input, state.config)),
  );
  const model = typeof config.model === 'string' ? config.model : config.model.id;
  if (!models.includes(model)) throw new Error('The proposed model is not available in this workspace.');
  if (tool.input.system !== undefined && config.system !== state.savedConfig?.system)
    config.system = builderFileDeliverySystem(config.system ?? '');
  const requirements = tool.input.environment_packages;
  const previousRequirements = state.config.metadata?.[environmentPackagesMetadataKey];
  const savedRequirements =
    requirements === undefined ? previousRequirements : JSON.stringify(environmentPackagesSchema.parse(requirements));
  if (savedRequirements !== undefined)
    config.metadata = { ...config.metadata, [environmentPackagesMetadataKey]: savedRequirements };
  return createAgentDraftSchema.parse(config);
}
export function validatedDeployment(tool: BuilderTool) {
  const input = deploymentInputSchema.parse(tool.input);
  const timezone = input.timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
  if (previewSchedule(input.cron_expression, timezone).error)
    throw new Error('Invalid deployment schedule or timezone.');
  return { ...input, timezone };
}
export function builderTools(models: string[]) {
  return Object.entries(builderToolInputs).map(([name, schema]) => {
    const official = platformQuickstartTools.find((tool) => tool.name === name);
    const inputSchema = z.toJSONSchema(schema, { unrepresentable: 'any' });
    if (name === 'build_agent_config' && inputSchema.properties)
      inputSchema.properties.model = { type: 'string', enum: models };
    return {
      name,
      description:
        (name === 'build_agent_config' ? configurationDescription : official?.description) ??
        (name === 'pick_environment'
          ? 'Choose an existing environment or request a new one.'
          : 'Choose vaults by credential coverage, or request a new vault.'),
      input_schema: inputSchema,
    };
  });
}
