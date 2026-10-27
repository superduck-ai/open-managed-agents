import { useQuery } from '@tanstack/react-query';
import { useI18n } from '../../../../shared/i18n';
import { Button, ButtonLink } from '../../../../shared/ui/button';
import { listDeploymentRuns } from '../../api';
import { RequestPreview, quickstartCurl } from '../RequestPreview';
import { QuickstartApiKey } from '../QuickstartApiKey';
import type { BuilderState } from './model';
import type { DeploymentRunApiResponse } from '../../types';
import { ManagedErrorAlert } from '../../components/common';
import { errorMessage } from '../../utils';

export function BuilderIntegration({
  state,
  workspaceId,
  accountId,
}: {
  state: BuilderState;
  workspaceId: string;
  accountId: string;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const runs = useQuery({
    queryKey: ['builder-deployment-runs', accountId, workspaceId, state.deployment?.id],
    queryFn: () => listDeploymentRuns(state.deployment!.id, workspaceId),
    enabled: Boolean(state.deployment),
    retry: false,
  });
  const sessionId = state.sessionID || 'SESSION_ID';
  const examples = state.deployment
    ? [
        quickstartCurl(`/v1/deployment_runs?deployment_id=${encodeURIComponent(state.deployment.id)}`, workspaceId),
        quickstartCurl('/v1/sessions/SESSION_ID/events/stream', workspaceId, undefined, true),
      ]
    : [
        quickstartCurl('/v1/sessions', workspaceId, {
          agent: { type: 'agent', id: state.agent?.id, version: state.agent?.version },
          environment_id: state.environment?.id,
          vault_ids: state.vaults.map((vault) => vault.id),
          resources: [],
          metadata: {},
        }),
        quickstartCurl(`/v1/sessions/${encodeURIComponent(sessionId)}/events/stream`, workspaceId, undefined, true),
        quickstartCurl(`/v1/sessions/${encodeURIComponent(sessionId)}/events`, workspaceId, {
          events: [
            { type: 'user.message', content: [{ type: 'text', text: state.firstMessage || state.suggestedMessage }] },
          ],
        }),
      ];
  return (
    <div className="space-y-5">
      <QuickstartApiKey workspaceID={workspaceId} />
      {state.deployment && (
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="font-medium">{zh ? '运行记录' : 'Runs'}</h3>
            <Button variant="ghost" size="sm" disabled={runs.isFetching} onClick={() => void runs.refetch()}>
              {zh ? '刷新' : 'Refresh'}
            </Button>
          </div>
          {runs.isPending ? (
            <p aria-busy="true">{zh ? '加载中…' : 'Loading…'}</p>
          ) : runs.isError ? (
            <ManagedErrorAlert>{errorMessage(runs.error)}</ManagedErrorAlert>
          ) : (
            <DeploymentRunsList runs={runs.data?.data ?? []} workspaceId={workspaceId} />
          )}
        </div>
      )}
      {examples.map((code, index) => (
        <RequestPreview key={index} code={code} hint={false} />
      ))}
      <ButtonLink
        href={`/workspaces/${encodeURIComponent(workspaceId)}/agents/${encodeURIComponent(state.agent?.id ?? '')}`}
      >
        {zh ? '查看智能体' : 'View agent'}
      </ButtonLink>
    </div>
  );
}

function DeploymentRunsList({ runs, workspaceId }: { runs: DeploymentRunApiResponse[]; workspaceId: string }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  if (!runs.length) return <p className="text-sm text-muted-foreground">{zh ? '暂无运行记录' : 'No runs yet'}</p>;
  return (
    <ul className="divide-y-[0.5px] divide-border">
      {runs.map((run) => (
        <li key={run.id} className="flex flex-wrap items-center justify-between gap-2 py-3">
          <time className="text-xs text-muted-foreground" dateTime={run.created_at}>
            {new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(
              new Date(run.created_at),
            )}
          </time>
          {run.session_id ? (
            <ButtonLink
              variant="link"
              href={`/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(run.session_id)}`}
            >
              {zh ? '查看会话' : 'View session'}
            </ButtonLink>
          ) : (
            <span className="text-sm">
              {run.error ? (zh ? '运行失败' : 'Run failed') : zh ? '等待会话' : 'Waiting for session'}
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}
