import { SessionSeq } from "@deepseek-ai/dsh-session";
import type { SessionEvent } from "@deepseek-ai/dsh-session";
import { emptyDelivery, type DeliveryState } from "../src/ccrv2/protocol.ts";

export function logData<Fields extends object>(fields: Fields): SessionEvent & Fields {
  return {
    type: "turn/start", seq: SessionSeq(0), time: 0, data: { turn: 1 },
    ...fields,
  };
}

export function deliveryData(fields: Partial<DeliveryState>): DeliveryState {
  return { ...emptyDelivery(), ...fields };
}
