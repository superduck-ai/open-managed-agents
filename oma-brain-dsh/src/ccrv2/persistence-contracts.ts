import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";

export interface DshLogData {
  header: SessionHeader;
  inherited: number;
  events: readonly SessionEvent[];
}

export interface CcrPersistenceData {
  dsh_log: DshLogData;
}

export type CcrPersistArgs = {
  [Subtype in keyof CcrPersistenceData]: [subtype: Subtype, data: CcrPersistenceData[Subtype], agentId?: string];
}[keyof CcrPersistenceData];
