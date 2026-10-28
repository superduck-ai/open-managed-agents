import { z } from 'zod';
import type { CreateAgentInput } from '../../types';

export const environmentPackagesSchema = z
  .object({
    type: z.literal('packages'),
    apt: z.array(z.string().trim().min(1)).optional(),
    pip: z.array(z.string().trim().min(1)).optional(),
    npm: z.array(z.string().trim().min(1)).optional(),
    cargo: z.array(z.string().trim().min(1)).optional(),
    gem: z.array(z.string().trim().min(1)).optional(),
    go: z.array(z.string().trim().min(1)).optional(),
  })
  .strict();
export type EnvironmentPackages = z.infer<typeof environmentPackagesSchema>;
export const environmentPackagesMetadataKey = 'quickstart_environment_packages';
export function requiredEnvironmentPackages(config: Pick<CreateAgentInput, 'metadata'>): EnvironmentPackages {
  const raw = config.metadata?.[environmentPackagesMetadataKey];
  return raw === undefined ? { type: 'packages' } : environmentPackagesSchema.parse(JSON.parse(String(raw)));
}
export function assertEnvironmentPackages(config: unknown, required: EnvironmentPackages) {
  const parsed = z
    .object({ packages: environmentPackagesSchema.nullish() })
    .passthrough()
    .parse(config ?? {});
  const missing = (['apt', 'pip', 'npm', 'cargo', 'gem', 'go'] as const).flatMap((manager) =>
    (required[manager] ?? [])
      .filter((spec) => !parsed.packages?.[manager]?.includes(spec))
      .map((spec) => `${manager}: ${spec}`),
  );
  if (missing.length)
    throw new Error(
      `Environment package configuration does not cover ${missing.join(', ')}. Propose a new environment containing these requirements for confirmation; do not modify the shared environment or claim installation is complete.`,
    );
}
