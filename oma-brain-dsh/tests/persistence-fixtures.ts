import { SessionSeq } from "@deepseek-ai/dsh-session";
import type { SessionEvent } from "@deepseek-ai/dsh-session";

export function logData<Fields extends object>(fields: Fields): SessionEvent & Fields {
  return {
    type: "turn/start", seq: SessionSeq(0), time: 0, data: { turn: 1 },
    ...fields,
  };
}
