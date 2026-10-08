import { z } from 'zod';
import type { QuickstartDraft, QuickstartScenarioID } from './model';

const draftSchema = z.object({ name: z.string(), description: z.string(), modelID: z.string(), system: z.string() });
const progressSchema = z.object({
  version: z.literal(1),
  step: z.number().int().min(0).max(3),
  scenarioID: z.enum(['hello', 'research', 'analysis', 'tracking', 'review', 'custom']),
  drafts: z.record(z.string(), draftSchema),
  agentID: z.string(),
  environmentID: z.string(),
  environmentName: z.string(),
  sessionID: z.string(),
  sessionBinding: z.string(),
  unconfirmedMessage: z.string().default(''),
  pending: z.object({ kind: z.enum(['agent', 'environment', 'session']), id: z.string() }).nullable(),
});

export type QuickstartProgress = z.infer<typeof progressSchema>;

export function initialQuickstartProgress(draft: QuickstartDraft): QuickstartProgress {
  return {
    version: 1,
    step: 0,
    scenarioID: 'hello',
    drafts: { hello: draft },
    agentID: '',
    environmentID: '',
    environmentName: 'Quickstart environment',
    sessionID: '',
    sessionBinding: '',
    unconfirmedMessage: '',
    pending: null,
  };
}

export function loadQuickstartProgress(key: string): QuickstartProgress | null {
  try {
    const saved = window.sessionStorage.getItem(key);
    if (!saved) return null;
    const result = progressSchema.safeParse(JSON.parse(saved));
    return result.success && result.data.drafts[result.data.scenarioID] ? result.data : null;
  } catch {
    return null;
  }
}

export function storeQuickstartProgress(key: string, progress: QuickstartProgress) {
  try {
    window.sessionStorage.setItem(key, JSON.stringify(progress));
    return true;
  } catch {
    return false;
  }
}

export function chooseQuickstartScenario(
  progress: QuickstartProgress,
  scenarioID: QuickstartScenarioID,
  draft: QuickstartDraft,
): QuickstartProgress {
  if (scenarioID === progress.scenarioID) return progress;
  return {
    ...progress,
    scenarioID,
    drafts: { ...progress.drafts, [scenarioID]: progress.drafts[scenarioID] ?? draft },
    agentID: '',
  };
}
