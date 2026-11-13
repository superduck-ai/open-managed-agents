import { consumeInbound, type CcrClient, type CcrSession } from "./ccrv2/index.ts";
import type { TaskAdmissionStage } from "./claim.ts";

export async function openWorkerSession(
  session: CcrSession,
  client: CcrClient,
  watch: (task: Promise<void>) => void,
  retire: (session: CcrSession) => Promise<void>,
  stage: (stage: TaskAdmissionStage) => void,
): Promise<void> {
  let inbound: Promise<void> | undefined;
  const startInbound = () => {
    if (inbound) return;
    inbound = consumeInbound(client, session).finally(() => retire(session));
    watch(inbound);
  };
  session.onRecoveryBlocked(startInbound);
  stage("open_session");
  await session.open();
  stage("start_inbound");
  startInbound();
}
