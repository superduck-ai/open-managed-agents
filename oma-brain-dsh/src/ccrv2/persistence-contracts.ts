import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";
import type { DeliveryState } from "./protocol.ts";

export type DshLogData = SessionHeader | SessionEvent;

export interface CcrPersistenceData {
  dsh_log: DshLogData;
  ccrv2_delivery: DeliveryState;
}

export type CcrPersistArgs = {
  [Type in keyof CcrPersistenceData]: [type: Type, data: CcrPersistenceData[Type], agentId?: string];
}[keyof CcrPersistenceData];
