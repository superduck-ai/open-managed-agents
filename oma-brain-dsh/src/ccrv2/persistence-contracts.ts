import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";
import type { BrainState } from "./protocol.ts";

export interface DshLogData {
  header: SessionHeader;
  inherited: number;
  events: readonly SessionEvent[];
}

export interface CcrPersistenceData {
  dsh_log: DshLogData;
  dsh_brain_state: BrainState;
}

export type CcrPersistArgs = {
  [Subtype in keyof CcrPersistenceData]: [subtype: Subtype, data: CcrPersistenceData[Subtype], agentId?: string];
}[keyof CcrPersistenceData];
