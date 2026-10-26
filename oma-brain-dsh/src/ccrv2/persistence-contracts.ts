import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";

export type DshLogData = SessionHeader | SessionEvent;

export interface CcrPersistenceData {
  dsh_log: DshLogData;
}

export type CcrPersistArgs = {
  [Type in keyof CcrPersistenceData]: [type: Type, data: CcrPersistenceData[Type], agentId?: string];
}[keyof CcrPersistenceData];
