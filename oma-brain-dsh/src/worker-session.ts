import { consumeInbound, type CcrClient, type CcrSession } from "./ccrv2/index.ts";
import type { TaskAdmissionStage } from "./transport.ts";

export async function openWorkerSession(
  session: CcrSession,
  client: CcrClient,
  watch: (task: Promise<void>) => void,
  retire: (session: CcrSession) => Promise<void>,
  stage: (stage: TaskAdmissionStage) => void,
): Promise<void> {
  stage("open_session");
  await session.open();
  stage("start_inbound");
  watch(consumeInbound(client, session).finally(() => retire(session)));
}
