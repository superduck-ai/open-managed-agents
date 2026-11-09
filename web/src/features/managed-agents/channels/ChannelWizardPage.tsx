import { useQuery } from '@tanstack/react-query';
import { useLocation, useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { useEffect } from 'react';
import { useI18n } from '../../../shared/i18n';
import { useWorkspace } from '../../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../../shared/workspaces/presentation';
import { channelApi, type Channel } from './api';
import { type WizardValue } from './ChannelWizardForm';
import { ChannelWizardFormPage } from './ChannelWizardFormPage';

type WizardSearch = { agentId?: string };

export function ChannelWizardPage({ mode }: { mode: 'create' | 'edit' }) {
  const { msg } = useI18n();
  const location = useLocation();
  const navigate = useNavigate();
  const params = useParams({ strict: false }) as { agentId?: string; channelId?: string };
  const search = useSearch({ strict: false }) as WizardSearch;
  const { activeWorkspaceId, selectWorkspace } = useWorkspace();
  const routeWorkspaceId = workspaceIdFromPath(location.pathname);
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) selectWorkspace(routeWorkspaceId);
  }, [routeWorkspaceId, activeWorkspaceId, selectWorkspace]);

  const agentId = mode === 'edit' ? params.agentId ?? '' : search.agentId ?? params.agentId ?? '';
  const channelId = mode === 'edit' ? params.channelId ?? '' : '';

  return (
    <ChannelWizardContent
      mode={mode}
      workspaceId={workspaceId}
      initialAgentId={agentId}
      channelId={channelId}
      onNavigate={(href) => void navigate({ href })}
      title={
        mode === 'edit'
          ? msg('managedAgents.channels.editPage', '编辑渠道')
          : msg('managedAgents.channels.createPage', '渠道创建页')
      }
    />
  );
}

export function ChannelWizardContent({
  mode,
  workspaceId,
  initialAgentId,
  channelId,
  onNavigate,
  title,
}: {
  mode: 'create' | 'edit';
  workspaceId: string;
  initialAgentId: string;
  channelId: string;
  onNavigate: (href: string) => void;
  title: string;
}) {
  const { msg } = useI18n();
  const existingQuery = useQuery({
    queryKey: ['channel-detail', workspaceId, initialAgentId, channelId],
    enabled: mode === 'edit' && Boolean(initialAgentId && channelId),
    queryFn: () => loadChannelForEdit(initialAgentId, workspaceId, channelId),
  });

  if (mode === 'edit' && existingQuery.isPending) {
    return (
      <section className="mx-auto w-full max-w-[880px] px-6 py-6 text-sm text-muted-foreground">
        {msg('managedAgents.channels.loading', '正在加载渠道…')}
      </section>
    );
  }
  if (mode === 'edit' && !existingQuery.data?.channel) {
    return (
      <section className="mx-auto w-full max-w-[880px] px-6 py-6 text-sm text-muted-foreground">
        {msg('managedAgents.channels.notFound', '未找到该渠道')}
      </section>
    );
  }

  const channel = existingQuery.data?.channel ?? null;
  const initialValue = buildInitialWizardValue(channel, initialAgentId, existingQuery.data?.environmentId ?? '');

  return (
    <ChannelWizardFormPage
      key={`${mode}:${channel?.id ?? 'new'}:${initialValue.environmentId}`}
      mode={mode}
      workspaceId={workspaceId}
      initialAgentId={initialAgentId}
      initialChannel={channel}
      initialValue={initialValue}
      onNavigate={onNavigate}
      title={title}
    />
  );
}

async function loadChannelForEdit(agentId: string, workspaceId: string, channelId: string) {
  const list = await channelApi<{ data: Channel[] }>(agentId, workspaceId);
  const channel = list.data.find((item) => item.id === channelId) ?? null;
  const settings = await channelApi<{ environment_id: string | null }>(agentId, workspaceId, '/settings');
  return { channel, environmentId: settings.environment_id };
}

function buildInitialWizardValue(
  channel: Channel | null,
  initialAgentId: string,
  environmentId: string,
): WizardValue {
  return {
    name: channel?.name ?? '',
    mode: channel?.mode ?? 'open',
    include_thinking: channel?.include_thinking ?? true,
    include_tool_calls: channel?.include_tool_calls ?? true,
    reply_in_thread: channel?.reply_in_thread ?? true,
    app_id: channel?.app_id ?? '',
    app_secret: '',
    channelType: 'feishu',
    agentId: initialAgentId,
    environmentId,
  };
}
