import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useI18n } from '../../../../shared/i18n';
import { Button, ButtonLink } from '../../../../shared/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../../shared/ui/select';
import { Field, FieldLabel } from '../../../../shared/ui/field';
import { Input } from '../../../../shared/ui/input';
import { listDeploymentRuns } from '../../api';
import { RequestPreview } from '../RequestPreview';
import { QuickstartApiKey } from '../QuickstartApiKey';
import type { BuilderState } from './model';
import type { DeploymentRunApiResponse } from '../../types';
import { ManagedErrorAlert } from '../../components/common';
import { errorMessage } from '../../utils';
import { builderIntegrationRequests, integrationExample, type IntegrationLanguage } from './integrationExamples';

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
  const [language, setLanguage] = useState<IntegrationLanguage>('bash');
  const [sessionId, setSessionId] = useState('');
  const runs = useQuery({
    queryKey: ['builder-deployment-runs', accountId, workspaceId, state.deployment?.id],
    queryFn: () => listDeploymentRuns(state.deployment!.id, workspaceId),
    enabled: Boolean(state.deployment),
    retry: false,
  });
  const examples = builderIntegrationRequests(state, sessionId);
  const titles = zh
    ? [state.deployment ? '查询运行记录' : '创建会话', '监听事件', '发送消息']
    : [state.deployment ? 'Query deployment runs' : 'Create a session', 'Listen for events', 'Send a message'];
  return (
    <div className="space-y-6 text-sm">
      <QuickstartApiKey workspaceID={workspaceId} />
      <Select
        value={language}
        onValueChange={(value) => {
          if (value) setLanguage(value as IntegrationLanguage);
        }}
      >
        <SelectTrigger aria-label={zh ? '代码语言' : 'Code language'}>
          <SelectValue>{language === 'bash' ? 'curl' : language === 'python' ? 'Python' : 'JavaScript'}</SelectValue>
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="bash">curl</SelectItem>
          <SelectItem value="javascript">JavaScript</SelectItem>
          <SelectItem value="python">Python</SelectItem>
        </SelectContent>
      </Select>
      {state.deployment && (
        <div className="space-y-2">
          <div className="flex items-center justify-between gap-2">
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
            <DeploymentRunsList runs={runs.data?.data ?? []} workspaceId={workspaceId} onSelect={setSessionId} />
          )}
        </div>
      )}
      {examples.map((request, index) => (
        <section key={request.path} className="min-w-0 space-y-3">
          <h3 className="font-medium">{titles[index]}</h3>
          {index === 1 && (
            <Field>
              <FieldLabel htmlFor="builder-integration-session">
                {zh ? '用于调用的 Session ID' : 'Session ID for these requests'}
              </FieldLabel>
              <Input
                id="builder-integration-session"
                value={sessionId}
                placeholder="SESSION_ID"
                onChange={(event) => setSessionId(event.target.value.trim())}
                aria-describedby="builder-integration-session-hint"
              />
              <p id="builder-integration-session-hint" className="text-xs text-muted-foreground">
                {state.deployment
                  ? zh
                    ? '使用上方运行记录返回的 session_id。尚无会话时，等待下一次运行后刷新。'
                    : 'Use session_id from a deployment run above. If none exists, refresh after the next run.'
                  : zh
                    ? '填入上一步创建会话响应中的 id。监听和发送共用这个新 ID，不使用旧试跑会话。'
                    : 'Use id from the create-session response above. Listening and sending use this new ID, separate from the earlier test.'}
              </p>
            </Field>
          )}
          <RequestPreview code={integrationExample(request, workspaceId, language)} language={language} hint={false} />
        </section>
      ))}
      <ButtonLink
        href={`/workspaces/${encodeURIComponent(workspaceId)}/agents/${encodeURIComponent(state.agent?.id ?? '')}`}
        target="_blank"
        rel="noopener noreferrer"
      >
        {zh ? '在管理页查看智能体' : 'Manage agent'}
      </ButtonLink>
    </div>
  );
}

function DeploymentRunsList({
  runs,
  workspaceId,
  onSelect,
}: {
  runs: DeploymentRunApiResponse[];
  workspaceId: string;
  onSelect: (id: string) => void;
}) {
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
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" onClick={() => onSelect(run.session_id!)}>
                {zh ? '使用此会话 ID' : 'Use this session ID'}
              </Button>
              <ButtonLink
                variant="link"
                target="_blank"
                rel="noopener noreferrer"
                href={`/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(run.session_id)}`}
              >
                {zh ? '查看会话' : 'View session'}
              </ButtonLink>
            </div>
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
