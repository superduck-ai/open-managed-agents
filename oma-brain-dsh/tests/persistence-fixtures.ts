import { SESSION_FORMAT_VERSION, SessionId } from "@deepseek-ai/dsh-session";
import type { DshLogData } from "../src/ccrv2/persistence-contracts.ts";
import { emptyState, type BrainState } from "../src/ccrv2/protocol.ts";

export function logData<Fields extends object>(fields: Fields, id = "s"): DshLogData & Fields {
  return {
    header: { version: SESSION_FORMAT_VERSION, id: SessionId(id), createdAt: 0, isSeeded: false },
    inherited: 0,
    events: [],
    ...fields,
  };
}

export function stateData(fields: Partial<BrainState>): BrainState {
  return { ...emptyState(), ...fields };
}
