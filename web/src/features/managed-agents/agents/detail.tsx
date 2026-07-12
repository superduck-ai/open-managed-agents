import { useI18n } from '../../../shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '../../../shared/ui/alert';
import { Badge } from '../../../shared/ui/badge';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../../../shared/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { Dialog, DialogClose, DialogContent, DialogHeader, DialogTitle } from '../../../shared/ui/dialog';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from '../../../shared/ui/dropdown-menu';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../../shared/ui/select';
import { toast } from '../../../shared/ui/sonner';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { useWorkspace } from '../../../shared/workspaces/context';
import clsx from 'clsx';
import { AlertCircle, Archive, ArrowUpRight, CalendarClock, ChevronDown, ChevronLeft, ChevronRight, Copy, MoreVertical, Pencil, Play, Plus, Sparkles, X } from 'lucide-react';
import { type ReactNode, useCallback, useEffect, useMemo, useState } from 'react';
import { agentEditConfig, agentEditConfigText, agentEditSaveErrorMessage, buildAgentUpdateInput, parseAgentEditConfigText } from '../agentConfig';
import { archiveAgent, createAgentDetailDeployment, createAgentDetailSession, getAgentSessionAnalyticsOverview, getAgentSessionAnalyticsTimeseries, listAgentDetailDeployments, listAgentDetailSessions, listAgentVersions, retrieveAgent, retrieveAgentSkill, runDeployment, updateAgentDetail, type AgentSkillApiResponse } from '../api';
import { AgentConfigEditor } from '../components/AgentConfigEditor';
import { ManagedDetailBreadcrumb } from '../components/breadcrumbs';
import { CopyButton, FormatSelect } from '../components/CodeBlocks';
import { ConfirmAgentsArchiveDialog, StatusPill } from '../components/common';
import { managedColumnLabel } from '../labels';
import { deploymentAgentVersion, DeploymentRunsPanel, deploymentTrigger, ManagedEntityDialog } from '../resources/ManagedResources';
import { numericValueFromKeys, stringValueFromKeys } from '../sessions/SessionDetailPage';
import { type AgentApiResponse, type AgentDetailCreatedFilter, type AgentDetailStatusFilter, type AgentDetailTab, type AgentDetailVersionFilter, type AgentSessionAnalyticsOverview, type AgentSessionAnalyticsTimeseries, type AnalyticsMetricBucket, type CodeFormat, type DeploymentApiResponse, type PageCursor, type SessionApiResponse } from '../types';
import { agentDetailHref, compactEntityId, copyText, errorMessage, managedEntityDetailHref, objectRecord, titleCase } from '../utils';
import {
  agentDetailDeploymentFromSearch,
  agentDetailSessionCreatedFromSearch,
  agentDetailSessionDeploymentFromSearch,
  agentDetailSessionStatusFromSearch,
  agentDetailSessionVersionFromSearch,
  agentDetailTabFromSearch,
  agentDetailVersionFromSearch,
  agentModelName,
  agentSkillId,
  agentSkillLabel,
  agentSkillRequestedVersion,
  agentSkillSnapshotSource,
  agentSkillSnapshotTitle,
  emptyAgentSessionAnalyticsOverview,
  ensureArray,
  formatAgentSkillSource,
  formatDecimal,
  formatDetailDate,
  formatDurationSeconds,
  formatInteger,
  formatPercent,
  latestAgentVersion,
  metricQuantile,
  metricTotal,
  metricValue,
  relativeTime,
  sessionTokenUsage,
  sessionVersionLabel,
  sortAgentVersions,
  uniqueVersionNumbers,
  writeAgentSessionFiltersToUrl
} from './model';
import { AgentToolsSection } from './tools/AgentToolsSection';
import { hasConfiguredAgentTools } from './tools/model';

export function AgentDetailPage({ agentId, routeWorkspaceId }: { agentId: string; routeWorkspaceId?: string }) {
  const { msg } = useI18n();
  const { activeWorkspaceId, orgUuid } = useWorkspace();
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  const [agent, setAgent] = useState<AgentApiResponse | null>(null);
  const [configAgent, setConfigAgent] = useState<AgentApiResponse | null>(null);
  const [versions, setVersions] = useState<AgentApiResponse[]>([]);
  const [selectedVersion, setSelectedVersion] = useState<number | null>(() => agentDetailVersionFromSearch());
  const [detailTab, setDetailTab] = useState<AgentDetailTab>(() => agentDetailTabFromSearch());
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [configLoadError, setConfigLoadError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [editOpen, setEditOpen] = useState(false);
  const [startSessionOpen, setStartSessionOpen] = useState(false);
  const [deploymentCreateRequest, setDeploymentCreateRequest] = useState(0);
  const [confirmArchiveOpen, setConfirmArchiveOpen] = useState(false);
  const [archiving, setArchiving] = useState(false);

  useEffect(() => {
    let active = true;
    setLoading(true);

    const latestAgentPromise = retrieveAgent(agentId, workspaceId, null);
    const selectedAgentPromise = selectedVersion
      ? retrieveAgent(agentId, workspaceId, selectedVersion)
        .then((value) => ({ value }))
        .catch((error: unknown) => ({ error }))
      : latestAgentPromise.then((value) => ({ value }));

    void Promise.all([latestAgentPromise, selectedAgentPromise, listAgentVersions(agentId, workspaceId)])
      .then(([loadedAgent, selectedAgentResult, versionPage]) => {
        if (!active) {
          return;
        }
        setAgent(loadedAgent);
        if ('error' in selectedAgentResult) {
          setConfigAgent(null);
          setConfigLoadError(errorMessage(selectedAgentResult.error));
        } else {
          setConfigAgent(selectedAgentResult.value);
          setConfigLoadError(null);
        }
        setVersions(sortAgentVersions(versionPage.data ?? [], loadedAgent));
        setLoadError(null);
        setLoading(false);
      })
      .catch((error: unknown) => {
        if (!active) {
          return;
        }
        setAgent(null);
        setConfigAgent(null);
        setVersions([]);
        setLoadError(errorMessage(error));
        setConfigLoadError(null);
        setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [agentId, refreshKey, selectedVersion, workspaceId]);

  const listHref = `/workspaces/${encodeURIComponent(workspaceId || 'default')}/agents`;
  const latestVersion = latestAgentVersion(versions, agent);
  const activeVersion = selectedVersion ?? configAgent?.version ?? agent?.version ?? latestVersion;
  const canEdit = Boolean(agent && !agent.archived_at);

  const writeDetailUrl = (tab: AgentDetailTab, version: number | null, options: { createDeployment?: boolean } = {}) => {
    if (typeof window === 'undefined') {
      return;
    }
    const url = new URL(window.location.href);
    url.searchParams.set('tab', tab);
    if (version) {
      url.searchParams.set('version_id', String(version));
    } else {
      url.searchParams.delete('version_id');
    }
    if (options.createDeployment) {
      url.searchParams.set('create_deployment', '1');
    } else {
      url.searchParams.delete('create_deployment');
    }
    window.history.replaceState(null, '', `${url.pathname}${url.search}${url.hash}`);
  };

  const selectTab = (tab: AgentDetailTab, options: { createDeployment?: boolean } = {}) => {
    setDetailTab(tab);
    writeDetailUrl(tab, selectedVersion, options);
  };

  const handleSaved = (updated: AgentApiResponse) => {
    setAgent(updated);
    setConfigAgent(updated);
    setConfigLoadError(null);
    setSelectedVersion(null);
    writeDetailUrl(detailTab, null);
    setEditOpen(false);
    toast.success(msg('managedAgents.agents.toastUpdated', 'Agent updated'));
    setRefreshKey((value) => value + 1);
  };

  const handleSelectVersion = (version: number) => {
    const nextVersion = version === latestVersion ? null : version;
    setSelectedVersion(nextVersion);
    writeDetailUrl(detailTab, nextVersion);
  };

  const handleCreateDeploymentAction = () => {
    setDeploymentCreateRequest((value) => value + 1);
    selectTab('deployments', { createDeployment: true });
  };

  const handleArchiveAgent = async () => {
    if (!agent || archiving) {
      return;
    }
    setArchiving(true);
    try {
      const archived = await archiveAgent(agent.id, workspaceId);
      setAgent(archived);
      toast.success(msg('managedAgents.agents.toastArchived', 'Agent archived'));
      setConfirmArchiveOpen(false);
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setArchiving(false);
    }
  };

  if (loading) {
    return (
      <section className="min-h-[calc(100vh-48px)] text-foreground">
        <ManagedDetailBreadcrumb listHref={listHref} listLabel={msg('managedAgents.agents.title', 'Agents')} />
        <div className="mt-14 text-sm text-muted-foreground">{msg('managedAgents.agents.loadingSingle', 'Loading agent...')}</div>
      </section>
    );
  }

  if (!agent || loadError) {
    return (
      <section className="min-h-[calc(100vh-48px)] text-foreground">
        <ManagedDetailBreadcrumb
          listHref={listHref}
          listLabel={msg('managedAgents.agents.title', 'Agents')}
          currentLabel={msg('common.error', 'Error')}
        />
        <AgentDetailErrorAlert className="mt-6 max-w-xl">
          {loadError || `Agent not found: ${agentId}`}
        </AgentDetailErrorAlert>
      </section>
    );
  }

  return (
    <section className="min-h-[calc(100vh-48px)] text-foreground">
      <ManagedDetailBreadcrumb
        listHref={listHref}
        listLabel={msg('managedAgents.agents.title', 'Agents')}
        currentLabel={agent.name || agent.id}
        className="mb-5"
      />

      <header className="mb-7 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="truncate text-[28px] font-semibold leading-tight text-foreground">
              {agent.name || msg('managedAgents.agents.untitled', 'Untitled agent')}
            </h1>
            <StatusPill>{agent.archived_at ? msg('common.archived', 'Archived') : msg('common.active', 'Active')}</StatusPill>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
            <Button
              type="button"
              aria-label={msg('common.copyId', 'Copy ID')}
              variant="outline"
              size="xs"
              className="max-w-[360px] font-sans text-[13px] text-foreground"
              onClick={() => void copyText(agent.id)}
            >
              <Copy className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
              <span className="truncate">{agent.id}</span>
            </Button>
            <span className="text-muted-foreground/70">.</span>
            <span>{msg('managedAgents.common.lastUpdatedAt', 'Last updated {date}', { date: formatDetailDate(agent.updated_at) })}</span>
          </div>
          {agent.description ? <p className="mt-3 max-w-[920px] text-[15px] leading-5 text-muted-foreground">{agent.description}</p> : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button
            type="button"
            disabled={!canEdit}
            size="lg"
            onClick={() => setEditOpen(true)}
          >
            <Pencil className="size-4" aria-hidden />
            {msg('common.edit', 'Edit')}
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  type="button"
                  variant="outline"
                  size="icon-lg"
                  aria-label={msg('managedAgents.common.moreActions', 'More actions')}
                  className="text-foreground"
                />
              }
            >
              <MoreVertical className="size-4" aria-hidden />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-52 bg-popover">
              <DropdownMenuItem
                disabled={Boolean(agent.archived_at)}
                onClick={() => {
                  setStartSessionOpen(true);
                }}
              >
                <Play className="size-4" aria-hidden />
                {msg('managedAgents.sessions.startSession', 'Start session')}
              </DropdownMenuItem>
              <DropdownMenuItem
                disabled={Boolean(agent.archived_at)}
                onClick={() => {
                  window.location.assign(`${agentDetailHref(workspaceId, agent.id)}/guided-edit`);
                }}
              >
                <Sparkles className="size-4" aria-hidden />
                {msg('managedAgents.agents.guidedEdit', 'Guided edit')}
              </DropdownMenuItem>
              <DropdownMenuItem disabled={Boolean(agent.archived_at)} onClick={handleCreateDeploymentAction}>
                <CalendarClock className="size-4" aria-hidden />
                {msg('managedAgents.deployments.createDeployment', 'Create deployment')}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="destructive"
                disabled={Boolean(agent.archived_at)}
                onClick={() => {
                  setConfirmArchiveOpen(true);
                }}
              >
                <Archive className="size-4" aria-hidden />
                {msg('common.archive', 'Archive')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>

      <Tabs
        value={detailTab}
        onValueChange={(nextValue) => selectTab(nextValue as AgentDetailTab)}
        className="gap-0"
      >
        <TabsList
          variant="line"
          aria-label={msg('managedAgents.agents.detail.sections', 'Agent detail sections')}
          className="mb-6 h-auto w-full justify-start gap-6 rounded-none border-b border-border p-0"
        >
          <TabsTrigger
            value="config"
            className="h-11 flex-none rounded-none border-0 px-0 text-sm font-semibold text-muted-foreground hover:bg-transparent hover:text-foreground data-active:bg-transparent data-active:text-foreground data-active:shadow-none after:bottom-0 after:h-px"
          >
            {msg('managedAgents.common.agent', 'Agent')}
          </TabsTrigger>
          <TabsTrigger
            value="sessions"
            className="h-11 flex-none rounded-none border-0 px-0 text-sm font-semibold text-muted-foreground hover:bg-transparent hover:text-foreground data-active:bg-transparent data-active:text-foreground data-active:shadow-none after:bottom-0 after:h-px"
          >
            {msg('managedAgents.sessions.title', 'Sessions')}
          </TabsTrigger>
          <TabsTrigger
            value="deployments"
            className="h-11 flex-none rounded-none border-0 px-0 text-sm font-semibold text-muted-foreground hover:bg-transparent hover:text-foreground data-active:bg-transparent data-active:text-foreground data-active:shadow-none after:bottom-0 after:h-px"
          >
            {msg('managedAgents.deployments.title', 'Deployments')}
          </TabsTrigger>
          <TabsTrigger
            value="observability"
            className="h-11 flex-none rounded-none border-0 px-0 text-sm font-semibold text-muted-foreground hover:bg-transparent hover:text-foreground data-active:bg-transparent data-active:text-foreground data-active:shadow-none after:bottom-0 after:h-px"
          >
            <span className="inline-flex items-center gap-2">
              {msg('managedAgents.observability.title', 'Observability')}
              <Badge variant="secondary" className="h-auto rounded-full px-1.5 py-0.5 text-[10px] font-semibold leading-none text-secondary-foreground">
                {msg('common.new', 'New')}
              </Badge>
            </span>
          </TabsTrigger>
        </TabsList>

        <TabsContent value="config" className="mt-0">
          {detailTab === 'config' ? (
            configLoadError ? (
              <AgentDetailErrorAlert
                title={msg('managedAgents.agents.detail.versionNotFound', 'Agent version not found')}
                className="max-w-xl"
              >
                {configLoadError || msg('managedAgents.agents.detail.versionLoadFailed', 'Failed to load agent version')}
              </AgentDetailErrorAlert>
            ) : (
              <AgentConfigTab
                agent={configAgent ?? agent}
                orgUuid={orgUuid ?? ''}
                workspaceId={workspaceId}
                versions={versions}
                activeVersion={activeVersion}
                latestVersion={latestVersion}
                onSelectVersion={handleSelectVersion}
              />
            )
          ) : null}
        </TabsContent>

        <TabsContent value="sessions" className="mt-0">
          {detailTab === 'sessions' ? (
            <AgentSessionsTab agentId={agent.id} workspaceId={workspaceId} versions={versions} />
          ) : null}
        </TabsContent>

        <TabsContent value="deployments" className="mt-0">
          {detailTab === 'deployments' ? (
            <AgentDeploymentsTab
              agent={agent}
              workspaceId={workspaceId}
              createRequest={deploymentCreateRequest}
              onCreateRequestHandled={() => {
                setDeploymentCreateRequest(0);
                writeDetailUrl('deployments', selectedVersion);
              }}
            />
          ) : null}
        </TabsContent>

        <TabsContent value="observability" className="mt-0">
          {detailTab === 'observability' ? (
            <AgentObservabilityTab agentId={agent.id} orgUuid={orgUuid} />
          ) : null}
        </TabsContent>
      </Tabs>

      {editOpen ? (
        <AgentEditDialog agent={agent} workspaceId={workspaceId} onClose={() => setEditOpen(false)} onSaved={handleSaved} />
      ) : null}
      {startSessionOpen ? (
        <ManagedEntityDialog
          section="sessions"
          title={msg('managedAgents.sessions.createLabel', 'Create session')}
          lockedAgent={agent}
          workspaceId={workspaceId}
          onClose={() => setStartSessionOpen(false)}
          onSubmit={async (values) => {
            const session = await createAgentDetailSession(agent, values, workspaceId);
            setStartSessionOpen(false);
            toast.success(msg('managedAgents.sessions.toastCreated', 'Session started'));
            window.history.pushState(null, '', `${managedEntityDetailHref(workspaceId, 'sessions', session.id)}?interactive=true`);
            const event = typeof PopStateEvent === 'function' ? new PopStateEvent('popstate') : new Event('popstate');
            window.dispatchEvent(event);
          }}
        />
      ) : null}
      {confirmArchiveOpen ? (
        <ConfirmAgentsArchiveDialog
          count={1}
          busy={archiving}
          onCancel={() => setConfirmArchiveOpen(false)}
          onConfirm={() => void handleArchiveAgent()}
        />
      ) : null}
    </section>
  );
}

export function AgentConfigTab({
  agent,
  orgUuid,
  workspaceId,
  versions,
  activeVersion,
  latestVersion,
  onSelectVersion
}: {
  agent: AgentApiResponse;
  orgUuid: string;
  workspaceId: string;
  versions: AgentApiResponse[];
  activeVersion: number;
  latestVersion: number;
  onSelectVersion: (version: number) => void;
}) {
  const { msg } = useI18n();
  const skills = ensureArray(agent.skills);
  const skillRefs = useMemo(
    () =>
      skills.map((skill, index) => ({
        key: `${agentSkillLabel(skill)}-${index}`,
        id: agentSkillId(skill),
        fallbackLabel: agentSkillLabel(skill),
        requestedVersion: agentSkillRequestedVersion(skill),
        snapshotSource: agentSkillSnapshotSource(skill),
        snapshotTitle: agentSkillSnapshotTitle(skill)
      })),
    [skills]
  );
  const skillIdsKey = useMemo(
    () => Array.from(new Set(skillRefs.map((skill) => skill.id).filter(Boolean))).join('\u0000'),
    [skillRefs]
  );
  const [skillDetailsById, setSkillDetailsById] = useState<Record<string, AgentSkillApiResponse>>({});
  const [skillDetailErrorsById, setSkillDetailErrorsById] = useState<Record<string, true>>({});
  const [skillDetailsLoading, setSkillDetailsLoading] = useState(false);
  const modelRecord = objectRecord(agent.model);
  const fastModel = modelRecord.speed === 'fast';

  useEffect(() => {
    const skillIds = skillIdsKey ? skillIdsKey.split('\u0000') : [];
    if (!skillIds.length) {
      setSkillDetailsById({});
      setSkillDetailErrorsById({});
      setSkillDetailsLoading(false);
      return;
    }

    let active = true;
    setSkillDetailsLoading(true);
    void Promise.all(
      skillIds.map(async (skillId) => {
        try {
          return { skillId, detail: await retrieveAgentSkill(skillId, workspaceId), ok: true as const };
        } catch {
          return { skillId, ok: false as const };
        }
      })
    ).then((results) => {
      if (!active) {
        return;
      }
      const nextDetails: Record<string, AgentSkillApiResponse> = {};
      const nextErrors: Record<string, true> = {};
      results.forEach((result) => {
        if (result.ok) {
          nextDetails[result.skillId] = result.detail;
        } else {
          nextErrors[result.skillId] = true;
        }
      });
      setSkillDetailsById(nextDetails);
      setSkillDetailErrorsById(nextErrors);
      setSkillDetailsLoading(false);
    });

    return () => {
      active = false;
    };
  }, [skillIdsKey, workspaceId]);

  return (
    <div className="space-y-6">
      <div>
        <AgentVersionDropdown
          label={msg('managedAgents.agents.detail.versionLabel', 'Version: v{version}', { version: activeVersion })}
          versions={versions}
          activeVersion={activeVersion}
          latestVersion={latestVersion}
          onSelect={onSelectVersion}
        />
      </div>

      <AgentDetailSection title={msg('analytics.table.model', 'Model')}>
        <div className="flex items-center gap-2 font-sans text-[15px] leading-6 text-foreground">
          {agentModelName(agent.model) || '-'}
          {fastModel ? <Badge variant="secondary" className="h-auto rounded-md px-2 py-0.5 text-xs font-semibold text-secondary-foreground">Fast</Badge> : null}
        </div>
      </AgentDetailSection>

      <AgentDetailSection title={msg('managedAgents.agents.detail.systemPrompt', 'System prompt')}>
        <pre className="subtle-scrollbar max-h-[360px] overflow-auto rounded-lg border border-border bg-muted px-4 py-3 font-sans text-[13px] leading-5 text-foreground whitespace-pre-wrap">
          <code className="font-sans">{agent.system || msg('managedAgents.agents.detail.noSystemPrompt', 'No system prompt configured.')}</code>
        </pre>
      </AgentDetailSection>

      <AgentDetailSection
        title={msg('managedAgents.agents.detail.mcpsAndTools', 'MCPs and tools')}
        description={
          hasConfiguredAgentTools(agent)
            ? undefined
            : msg('managedAgents.agents.detail.noMcpsOrTools', 'No MCPs or tools configured.')
        }
      >
        {hasConfiguredAgentTools(agent) ? (
          <AgentToolsSection agent={agent} orgUuid={orgUuid} workspaceId={workspaceId} />
        ) : null}
      </AgentDetailSection>

      <AgentDetailSection
        title={msg('managedAgents.skills.title', 'Skills')}
        description={skillRefs.length ? undefined : msg('managedAgents.agents.detail.noSkills', 'No skills configured.')}
      >
        {skillRefs.length ? (
          <AgentSkillsList
            skills={skillRefs}
            detailsById={skillDetailsById}
            errorsById={skillDetailErrorsById}
            loading={skillDetailsLoading}
          />
        ) : null}
      </AgentDetailSection>
    </div>
  );
}

type AgentSkillRef = {
  key: string;
  id: string;
  fallbackLabel: string;
  requestedVersion: string;
  snapshotSource: string;
  snapshotTitle: string;
};

function SkillVersionBadges({
  requestedVersion,
  msg
}: {
  requestedVersion: string;
  msg: any;
}) {
  const isLatest = requestedVersion === 'latest' || !requestedVersion;

  return (
    <div className="mt-1 flex flex-wrap gap-1.5">
      <Badge
        variant="outline"
        className="h-auto rounded-md px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground bg-muted/30"
      >
        {isLatest ? msg('skills.versions.latest', 'Latest') : `v${requestedVersion}`}
      </Badge>
    </div>
  );
}

function AgentSkillsList({
  skills,
  detailsById,
  errorsById,
  loading
}: {
  skills: AgentSkillRef[];
  detailsById: Record<string, AgentSkillApiResponse>;
  errorsById: Record<string, true>;
  loading: boolean;
}) {
  const { msg } = useI18n();
  const [expandedSkillKey, setExpandedSkillKey] = useState<string | null>(null);

  const skillRows = skills.map((skill) => {
    const detail = skill.id ? detailsById[skill.id] : undefined;
    const displayTitle = detail?.display_title?.trim() || skill.snapshotTitle || skill.id || skill.fallbackLabel;
    const source = detail?.source || skill.snapshotSource;
    const requestedVersion = skill.requestedVersion || msg('managedAgents.agents.detail.skillLatestRequested', 'latest');
    const latestVersion = detail?.latest_version?.trim() || '';

    return {
      key: skill.key,
      displayTitle,
      idLabel: skill.id || skill.fallbackLabel,
      source,
      metadataUnavailable: Boolean(skill.id && errorsById[skill.id]),
      requestedVersion,
      latestVersion,
      createdAt: detail?.created_at || '',
      updatedAt: detail?.updated_at || '',
      copyId: skill.id
    };
  });

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <div className="divide-y divide-border">
        {skillRows.map((skill) => {
          const isExpanded = expandedSkillKey === skill.key;
          return (
            <Collapsible
              key={skill.key}
              open={isExpanded}
              onOpenChange={(open) => setExpandedSkillKey(open ? skill.key : null)}
              className="bg-card"
            >
              <div className="group/row flex items-center justify-between bg-card hover:bg-muted/50 transition-colors">
                <CollapsibleTrigger
                  type="button"
                  aria-label={msg('managedAgents.agents.detail.skillSummary', '{name} skill summary', { name: skill.displayTitle })}
                  className="flex h-auto flex-1 items-center gap-4 px-4 py-3 text-left text-sm font-normal text-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-3">
                    <span className="grid size-10 place-items-center rounded-lg border border-border bg-secondary text-foreground">
                      <Sparkles className="size-5 text-primary" aria-hidden />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="truncate text-sm font-semibold text-foreground">
                          {skill.displayTitle}
                        </span>
                        <span className="hidden text-[11px] text-muted-foreground sm:inline">
                          (
                          <code className="truncate font-mono">
                            {skill.idLabel}
                          </code>
                          )
                        </span>
                        <Badge
                          variant="secondary"
                          className="h-auto rounded-md px-1.5 py-0.5 text-[10px] font-normal text-muted-foreground bg-secondary/80"
                        >
                          {formatAgentSkillSource(skill.source)}
                        </Badge>
                      </div>
                      <SkillVersionBadges
                        requestedVersion={skill.requestedVersion}
                        msg={msg}
                      />
                    </div>
                  </div>
                </CollapsibleTrigger>
                <div className="flex shrink-0 items-center gap-2 pr-4">
                  {skill.copyId ? (
                    <div className="opacity-0 group-hover/row:opacity-100 focus-within:opacity-100 transition-opacity duration-150">
                      <CopyButton
                        value={skill.copyId}
                        label={msg('managedAgents.agents.detail.copySkillId', 'Copy skill ID')}
                      />
                    </div>
                  ) : null}
                  <CollapsibleTrigger
                    type="button"
                    className="grid size-8 place-items-center rounded-md hover:bg-accent text-muted-foreground/70"
                  >
                    <ChevronDown
                      className={clsx(
                        'size-4 transition',
                        isExpanded && 'rotate-180'
                      )}
                      aria-hidden
                    />
                  </CollapsibleTrigger>
                </div>
              </div>
              <CollapsibleContent className="border-t border-border bg-muted/30">
                <div className="px-5 py-4">
                  <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2 text-xs leading-5">
                    <div className="flex flex-col gap-1">
                      <dt className="font-medium text-muted-foreground">
                        {msg('managedAgents.agents.detail.skillIdLabel', 'ID')}
                      </dt>
                      <dd className="flex items-center gap-1.5 font-mono text-foreground break-all">
                        {skill.idLabel}
                        {skill.copyId ? (
                          <CopyButton
                            value={skill.copyId}
                            label={msg('managedAgents.agents.detail.copySkillId', 'Copy skill ID')}
                          />
                        ) : null}
                      </dd>
                    </div>

                    <div className="flex flex-col gap-1">
                      <dt className="font-medium text-muted-foreground">
                        {msg('managedAgents.agents.detail.skillSourceLabel', 'Source')}
                      </dt>
                      <dd className="text-foreground">
                        <Badge variant="secondary" className="h-auto rounded-md px-2 py-0.5 text-xs font-normal">
                          {formatAgentSkillSource(skill.source)}
                        </Badge>
                      </dd>
                    </div>

                    <div className="flex flex-col gap-1">
                      <dt className="font-medium text-muted-foreground">
                        {msg('managedAgents.agents.detail.skillAgentVersionLabel', 'Agent version')}
                      </dt>
                      <dd className="text-foreground font-medium">
                        {skill.requestedVersion}
                      </dd>
                    </div>

                    {skill.latestVersion ? (
                      <div className="flex flex-col gap-1">
                        <dt className="font-medium text-muted-foreground">
                          {msg('managedAgents.agents.detail.skillLatestVersionLabel', 'Latest version')}
                        </dt>
                        <dd className="text-foreground font-medium">
                          {skill.latestVersion}
                        </dd>
                      </div>
                    ) : null}

                    {skill.updatedAt ? (
                      <div className="flex flex-col gap-1">
                        <dt className="font-medium text-muted-foreground">
                          {msg('managedAgents.agents.detail.skillUpdatedLabel', 'Updated')}
                        </dt>
                        <dd className="text-foreground">
                          {formatDetailDate(skill.updatedAt)}
                        </dd>
                      </div>
                    ) : null}

                    {skill.createdAt ? (
                      <div className="flex flex-col gap-1">
                        <dt className="font-medium text-muted-foreground">
                          {msg('managedAgents.agents.detail.skillCreatedLabel', 'Created')}
                        </dt>
                        <dd className="text-foreground">
                          {formatDetailDate(skill.createdAt)}
                        </dd>
                      </div>
                    ) : null}

                    <div className="flex flex-col gap-1">
                      <dt className="font-medium text-muted-foreground">
                        {msg('managedAgents.agents.detail.skillMetadataLabel', 'Metadata')}
                      </dt>
                      <dd className="text-foreground">
                        {skill.metadataUnavailable ? (
                          <span className="text-destructive font-medium">
                            {msg('managedAgents.agents.detail.skillMetadataUnavailable', 'Metadata unavailable')}
                          </span>
                        ) : (
                          <span className="text-emerald-600 dark:text-emerald-400 font-medium">
                            {msg('managedAgents.agents.detail.skillMetadataAvailable', 'Resolved')}
                          </span>
                        )}
                      </dd>
                    </div>
                  </dl>
                </div>
              </CollapsibleContent>
            </Collapsible>
          );
        })}
      </div>
      {loading ? (
        <div className="border-t border-border px-4 py-2 text-xs text-muted-foreground">
          {msg('managedAgents.agents.detail.resolvingSkillMetadata', 'Resolving skill metadata...')}
        </div>
      ) : null}
    </Card>
  );
}

export function AgentDetailSection({
  title,
  description,
  action,
  children
}: {
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <section>
      <div className="mb-3 flex items-start justify-between gap-4">
        <h2 className="text-base font-semibold leading-6 text-foreground">{title}</h2>
        {action}
      </div>
      {description ? <p className={clsx('text-sm text-muted-foreground', children && 'mb-3')}>{description}</p> : null}
      {children}
    </section>
  );
}

export function AgentSessionsTab({
  agentId,
  workspaceId,
  versions
}: {
  agentId: string;
  workspaceId: string;
  versions: AgentApiResponse[];
}) {
  const { msg } = useI18n();
  const [createdFilter, setCreatedFilter] = useState<AgentDetailCreatedFilter>(() => agentDetailSessionCreatedFromSearch());
  const [versionFilter, setVersionFilter] = useState<AgentDetailVersionFilter>(() => agentDetailSessionVersionFromSearch());
  const [deploymentFilter, setDeploymentFilter] = useState(() => agentDetailSessionDeploymentFromSearch());
  const [statusFilter, setStatusFilter] = useState<AgentDetailStatusFilter>(() => agentDetailSessionStatusFromSearch());
  const [deployments, setDeployments] = useState<DeploymentApiResponse[]>([]);
  const [deploymentsLoading, setDeploymentsLoading] = useState(true);
  const [sessions, setSessions] = useState<SessionApiResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [pageState, setPageState] = useState<{ cursor: PageCursor; history: PageCursor[]; nextPage: PageCursor }>({
    cursor: null,
    history: [],
    nextPage: null
  });

  useEffect(() => {
    let active = true;
    setDeploymentsLoading(true);
    void listAgentDetailDeployments(agentId, workspaceId, { cursor: null })
      .then((page) => {
        if (!active) {
          return;
        }
        setDeployments(page.data ?? []);
        setDeploymentsLoading(false);
      })
      .catch(() => {
        if (!active) {
          return;
        }
        setDeployments([]);
        setDeploymentsLoading(false);
      });
    return () => {
      active = false;
    };
  }, [agentId, workspaceId]);

  useEffect(() => {
    let active = true;
    setLoading(true);

    void listAgentDetailSessions(agentId, workspaceId, {
      created: createdFilter,
      version: versionFilter,
      deploymentId: deploymentFilter,
      status: statusFilter,
      cursor: pageState.cursor
    })
      .then((page) => {
        if (!active) {
          return;
        }
        setSessions(page.data ?? []);
        setPageState((current) => ({ ...current, nextPage: page.next_page ?? null }));
        setLoadError(null);
        setLoading(false);
      })
      .catch((error: unknown) => {
        if (!active) {
          return;
        }
        setSessions([]);
        setLoadError(errorMessage(error));
        setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [agentId, createdFilter, deploymentFilter, pageState.cursor, statusFilter, versionFilter, workspaceId]);

  const resetPage = () => setPageState({ cursor: null, history: [], nextPage: null });
  const goNext = () => {
    if (!pageState.nextPage) {
      return;
    }
    setPageState((current) => ({
      cursor: current.nextPage,
      history: [...current.history, current.cursor],
      nextPage: null
    }));
  };
  const goPrevious = () => {
    if (!pageState.history.length) {
      return;
    }
    setPageState((current) => ({
      cursor: current.history[current.history.length - 1],
      history: current.history.slice(0, -1),
      nextPage: null
    }));
  };

  return (
    <div>
      <div className="mb-7 flex flex-wrap items-center gap-2">
        <AgentDetailCreatedFilterDropdown
          value={createdFilter}
          onSelect={(created) => {
            setCreatedFilter(created);
            resetPage();
            writeAgentSessionFiltersToUrl({ created, version: versionFilter, deploymentId: deploymentFilter, status: statusFilter });
          }}
        />
        <AgentVersionFilterDropdown
          versions={versions}
          value={versionFilter}
          onSelect={(version) => {
            setVersionFilter(version);
            resetPage();
            writeAgentSessionFiltersToUrl({ created: createdFilter, version, deploymentId: deploymentFilter, status: statusFilter });
          }}
        />
        <AgentDeploymentFilterDropdown
          deployments={deployments}
          value={deploymentFilter}
          loading={deploymentsLoading}
          onSelect={(deploymentId) => {
            setDeploymentFilter(deploymentId);
            resetPage();
            writeAgentSessionFiltersToUrl({ created: createdFilter, version: versionFilter, deploymentId, status: statusFilter });
          }}
        />
        <AgentStatusFilterDropdown
          value={statusFilter}
          onSelect={(status) => {
            setStatusFilter(status);
            resetPage();
            writeAgentSessionFiltersToUrl({ created: createdFilter, version: versionFilter, deploymentId: deploymentFilter, status });
          }}
        />
      </div>

      {loadError ? (
        <AgentDetailErrorAlert className="mb-4 max-w-xl">{loadError}</AgentDetailErrorAlert>
      ) : null}

      <Card className="gap-0 py-0">
        <table className="w-full table-fixed text-left text-sm">
          <thead>
            <tr className="border-b border-border text-muted-foreground">
              <th className="h-10 w-[48px] px-3 font-medium">
                <span className="block size-4 rounded border border-border" aria-hidden />
              </th>
              <th className="h-10 w-[210px] px-3 font-medium">{managedColumnLabel('ID', msg)}</th>
              <th className="h-10 px-3 font-medium">{managedColumnLabel('Name', msg)}</th>
              <th className="h-10 w-[150px] px-3 font-medium">{managedColumnLabel('Status', msg)}</th>
              <th className="h-10 w-[130px] px-3 font-medium">{msg('managedAgents.agents.detail.version', 'Version')}</th>
              <th className="h-10 w-[150px] px-3 font-medium">{msg('managedAgents.sessions.tokensInOut', 'Tokens in / out')}</th>
              <th className="h-10 w-[180px] px-3 font-medium">{managedColumnLabel('Created', msg)}</th>
              <th className="h-10 w-[48px] px-2 font-medium" aria-label={managedColumnLabel('Actions', msg)} />
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={8} className="h-32 px-4 text-center text-muted-foreground">
                  {msg('managedAgents.sessions.loading', 'Loading sessions...')}
                </td>
              </tr>
            ) : sessions.length ? (
              sessions.map((session) => {
                const usage = sessionTokenUsage(session);
                return (
                  <tr key={session.id} className="border-b border-border text-foreground last:border-b-0">
                    <td className="h-11 px-3 align-middle">
                      <span className="block size-4 rounded border border-border" aria-hidden />
                    </td>
                    <td className="h-11 truncate px-3 align-middle font-sans text-[13px] text-foreground">
                      {compactEntityId(session.id)}
                    </td>
                    <td className="h-11 truncate px-3 align-middle">{session.title || '-'}</td>
                    <td className="h-11 px-3 align-middle">
                      <StatusPill>{titleCase(session.status || 'idle')}</StatusPill>
                    </td>
                    <td className="h-11 px-3 align-middle">{sessionVersionLabel(session)}</td>
                    <td className="h-11 px-3 align-middle text-muted-foreground">{formatInteger(usage.input)} / {formatInteger(usage.output)}</td>
                    <td className="h-11 px-3 align-middle text-muted-foreground">{relativeTime(session.created_at)}</td>
                    <td className="h-11 px-2 align-middle">
                      <ButtonLink
                        href={`/workspaces/${encodeURIComponent(workspaceId)}/sessions/${encodeURIComponent(session.id)}`}
                        variant="ghost"
                        size="icon"
                        aria-label={msg('managedAgents.quickstart.viewSession', 'View session')}
                        className="text-foreground"
                      >
                        <ArrowUpRight className="size-4" aria-hidden />
                      </ButtonLink>
                    </td>
                  </tr>
                );
              })
            ) : (
              <tr>
                <td colSpan={8} className="h-36 px-4 text-center text-muted-foreground">
                  <strong className="block text-foreground">{msg('managedAgents.sessions.noSessionsForAgent', 'No sessions yet')}</strong>
                  <span className="mt-1 block">{msg('managedAgents.sessions.noSessionsForAgentBody', 'Run this agent to create a session.')}</span>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>

      <div className="mt-7 flex items-center gap-2">
        <Button
          type="button"
          disabled={!pageState.history.length || loading}
          variant="outline"
          size="icon-lg"
          aria-label={msg('pagination.previousPage', 'Previous page')}
          onClick={goPrevious}
        >
          <ChevronLeft className="size-4" aria-hidden />
        </Button>
        <Button
          type="button"
          disabled={!pageState.nextPage || loading}
          variant="outline"
          size="icon-lg"
          aria-label={msg('pagination.nextPage', 'Next page')}
          onClick={goNext}
        >
          <ChevronRight className="size-4" aria-hidden />
        </Button>
      </div>
    </div>
  );
}

export function AgentDeploymentsTab({
  agent,
  workspaceId,
  createRequest,
  onCreateRequestHandled
}: {
  agent: AgentApiResponse;
  workspaceId: string;
  createRequest: number;
  onCreateRequestHandled: () => void;
}) {
  const { msg } = useI18n();
  const [deployments, setDeployments] = useState<DeploymentApiResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [expandedDeploymentId, setExpandedDeploymentId] = useState<string | null>(() => agentDetailDeploymentFromSearch());
  const [refreshKey, setRefreshKey] = useState(0);
  const [pageState, setPageState] = useState<{ cursor: PageCursor; history: PageCursor[]; nextPage: PageCursor }>({
    cursor: null,
    history: [],
    nextPage: null
  });

  useEffect(() => {
    if (createRequest <= 0) {
      return;
    }
    setDialogOpen(true);
    onCreateRequestHandled();
  }, [createRequest, onCreateRequestHandled]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    const params = new URLSearchParams(window.location.search);
    if (params.get('create_deployment') !== '1') {
      return;
    }
    setDialogOpen(true);
    params.delete('create_deployment');
    const query = params.toString();
    window.history.replaceState(null, '', `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`);
  }, []);

  useEffect(() => {
    let active = true;
    setLoading(true);
    void listAgentDetailDeployments(agent.id, workspaceId, { cursor: pageState.cursor })
      .then((page) => {
        if (!active) {
          return;
        }
        setDeployments(page.data ?? []);
        setPageState((current) => ({ ...current, nextPage: page.next_page ?? null }));
        setLoadError(null);
        setLoading(false);
      })
      .catch((error: unknown) => {
        if (!active) {
          return;
        }
        setDeployments([]);
        setLoadError(errorMessage(error));
        setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [agent.id, pageState.cursor, refreshKey, workspaceId]);

  const resetPage = () => setPageState({ cursor: null, history: [], nextPage: null });
  const openDialog = () => setDialogOpen(true);
  const goNext = () => {
    if (!pageState.nextPage) {
      return;
    }
    setPageState((current) => ({
      cursor: current.nextPage,
      history: [...current.history, current.cursor],
      nextPage: null
    }));
  };
  const goPrevious = () => {
    if (!pageState.history.length) {
      return;
    }
    setPageState((current) => ({
      cursor: current.history[current.history.length - 1],
      history: current.history.slice(0, -1),
      nextPage: null
    }));
  };

  const selectDeployment = (deploymentId: string | null) => {
    setExpandedDeploymentId(deploymentId);
    if (typeof window === 'undefined') {
      return;
    }
    const url = new URL(window.location.href);
    if (deploymentId) {
      url.searchParams.set('deployment', deploymentId);
    } else {
      url.searchParams.delete('deployment');
    }
    window.history.replaceState(null, '', `${url.pathname}${url.search}${url.hash}`);
  };

  return (
    <div>
      {loading || loadError || deployments.length ? (
        <div className="mb-5 flex items-center justify-between gap-4">
          <div>
            <h2 className="text-base font-semibold leading-6 text-foreground">{msg('managedAgents.deployments.title', 'Deployments')}</h2>
            <p className="mt-1 text-sm leading-5 text-muted-foreground">
              {msg('managedAgents.deployments.agentDetailDescription', 'Run this agent on a schedule, via webhook, or manually.')}
            </p>
          </div>
          <Button
            type="button"
            disabled={Boolean(agent.archived_at)}
            size="lg"
            onClick={openDialog}
          >
            <Plus className="size-4" aria-hidden />
            {msg('managedAgents.deployments.createDeployment', 'Create deployment')}
          </Button>
        </div>
      ) : null}

      {loadError ? (
        <AgentDetailErrorAlert className="mb-4 max-w-xl">{loadError}</AgentDetailErrorAlert>
      ) : null}

      <Card className="gap-0 py-0">
        {loading ? (
          <div className="h-44 px-4 py-12 text-center text-sm text-muted-foreground">
            {msg('managedAgents.deployments.loading', 'Loading deployments...')}
          </div>
        ) : deployments.length ? (
          <div className="divide-y divide-border">
            {deployments.map((deployment) => (
              <Collapsible
                key={deployment.id}
                open={expandedDeploymentId === deployment.id}
                onOpenChange={(open) => selectDeployment(open ? deployment.id : null)}
                className="bg-card"
              >
                <CollapsibleTrigger
                  type="button"
                  aria-controls={`agent-deployment-panel-${deployment.id}`}
                  className="grid h-auto w-full grid-cols-[minmax(0,1.3fr)_120px_150px_150px_32px] items-center gap-4 px-4 py-3 text-left text-sm font-normal text-foreground transition-colors hover:bg-muted focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                >
                  <span className="min-w-0">
                    <span className="block truncate font-medium">{deployment.name || deployment.id}</span>
                    <span className="block truncate font-sans text-xs text-muted-foreground">{deployment.id}</span>
                  </span>
                  <StatusPill>{titleCase(deployment.status || 'active')}</StatusPill>
                  <span className="text-muted-foreground">{deploymentTrigger(deployment)}</span>
                  <span className="text-muted-foreground">{relativeTime(deployment.updated_at || deployment.created_at)}</span>
                  <ChevronDown className={clsx('size-4 justify-self-end text-muted-foreground/70 transition', expandedDeploymentId === deployment.id && 'rotate-180')} aria-hidden />
                </CollapsibleTrigger>
                <CollapsibleContent
                  id={`agent-deployment-panel-${deployment.id}`}
                  className="border-t border-border"
                >
                  <div className="px-4 py-4">
                    <AgentDeploymentDetailPanel
                      deployment={deployment}
                      workspaceId={workspaceId}
                      refreshKey={refreshKey}
                      onRefresh={() => setRefreshKey((value) => value + 1)}
                    />
                  </div>
                </CollapsibleContent>
              </Collapsible>
            ))}
          </div>
        ) : (
          <div className="flex min-h-[280px] flex-col items-center justify-center px-4 py-12 text-center">
            <span className="grid size-11 place-items-center rounded-full border border-border bg-secondary text-muted-foreground">
              <CalendarClock className="size-5" aria-hidden />
            </span>
            <h3 className="mt-4 text-base font-semibold text-foreground">{msg('managedAgents.deployments.noDeployments', 'No deployments')}</h3>
            <p className="mt-1 max-w-[420px] text-sm leading-5 text-muted-foreground">
              {msg('managedAgents.deployments.noDeploymentsBody', 'Deploy this agent to run it on a schedule, via webhook, or manually.')}
            </p>
            <Button
              type="button"
              disabled={Boolean(agent.archived_at)}
              size="lg"
              className="mt-5"
              onClick={openDialog}
            >
              <Plus className="size-4" aria-hidden />
              {msg('managedAgents.deployments.createDeployment', 'Create deployment')}
            </Button>
          </div>
        )}
      </Card>

      {deployments.length || pageState.history.length || pageState.nextPage ? (
        <div className="mt-7 flex items-center gap-2">
          <Button
            type="button"
            disabled={!pageState.history.length || loading}
            variant="outline"
            size="icon-lg"
            aria-label={msg('pagination.previousPage', 'Previous page')}
            onClick={goPrevious}
          >
            <ChevronLeft className="size-4" aria-hidden />
          </Button>
          <Button
            type="button"
            disabled={!pageState.nextPage || loading}
            variant="outline"
            size="icon-lg"
            aria-label={msg('pagination.nextPage', 'Next page')}
            onClick={goNext}
          >
            <ChevronRight className="size-4" aria-hidden />
          </Button>
        </div>
      ) : null}

      {dialogOpen ? (
        <ManagedEntityDialog
          section="deployments"
          title={msg('managedAgents.deployments.createDeployment', 'Create deployment')}
          lockedAgent={agent}
          workspaceId={workspaceId}
          onClose={() => setDialogOpen(false)}
          onSubmit={async (values) => {
            const deployment = await createAgentDetailDeployment(agent, values, workspaceId);
            setDialogOpen(false);
            resetPage();
            setRefreshKey((value) => value + 1);
            selectDeployment(deployment.id);
          }}
        />
      ) : null}
    </div>
  );
}

export function AgentDeploymentDetailPanel({
  deployment,
  workspaceId,
  refreshKey,
  onRefresh
}: {
  deployment: DeploymentApiResponse;
  workspaceId: string;
  refreshKey: number;
  onRefresh: () => void;
}) {
  const { msg } = useI18n();
  const [runningNow, setRunningNow] = useState(false);
  const [runError, setRunError] = useState<string | null>(null);

  const startRun = async () => {
    if (runningNow || deployment.archived_at) {
      return;
    }
    setRunningNow(true);
    setRunError(null);
    try {
      await runDeployment(deployment.id, workspaceId);
      onRefresh();
    } catch (error) {
      setRunError(errorMessage(error));
    } finally {
      setRunningNow(false);
    }
  };

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="text-sm text-muted-foreground">
          <span className="font-sans text-foreground">{deployment.id}</span>
          {typeof deploymentAgentVersion(deployment) === 'number' ? (
            <Badge variant="secondary" className="ml-2 h-auto rounded-md px-2 py-0.5 text-xs font-normal text-muted-foreground">
              v{deploymentAgentVersion(deployment)}
            </Badge>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <ButtonLink
            href={`/workspaces/${encodeURIComponent(workspaceId)}/sessions?deployment_id=${encodeURIComponent(deployment.id)}`}
            variant="secondary"
          >
            {msg('managedAgents.deployments.viewSessions', 'View sessions')}
          </ButtonLink>
          <Button
            type="button"
            disabled={runningNow || Boolean(deployment.archived_at)}
            onClick={() => void startRun()}
          >
            <Play className="size-3.5" aria-hidden />
            {runningNow ? msg('managedAgents.deployments.runningNow', 'Running...') : msg('managedAgents.deployments.runNow', 'Run now')}
          </Button>
        </div>
      </div>
      {runError ? <AgentDetailErrorAlert className="mb-4 max-w-xl">{runError}</AgentDetailErrorAlert> : null}
      <DeploymentRunsPanel deployment={deployment} workspaceId={workspaceId} refreshKey={refreshKey} />
    </div>
  );
}

export function AgentObservabilityTab({ agentId, orgUuid }: { agentId: string; orgUuid?: string }) {
  const { msg } = useI18n();
  const [overview, setOverview] = useState<AgentSessionAnalyticsOverview | null>(null);
  const [timeseries, setTimeseries] = useState<AgentSessionAnalyticsTimeseries | null>(null);
  const [loading, setLoading] = useState(Boolean(orgUuid));
  const [loadError, setLoadError] = useState<string | null>(null);
  const [groupBy, setGroupBy] = useState('agent_version');
  const groupByOptions = [
    { value: 'agent_version', label: msg('managedAgents.observability.groupByAgentVersion', 'Agent version') },
    { value: 'outcome_category', label: msg('managedAgents.observability.groupByOutcomeCategory', 'Outcome category') },
    { value: 'had_error', label: msg('managedAgents.observability.groupByHadError', 'Had error') }
  ];
  const selectedGroupBy = groupByOptions.find((option) => option.value === groupBy) ?? groupByOptions[0];

  useEffect(() => {
    if (!orgUuid) {
      setOverview(emptyAgentSessionAnalyticsOverview());
      setTimeseries({ data: [] });
      setLoading(false);
      return;
    }
    let active = true;
    setLoading(true);
    void Promise.all([getAgentSessionAnalyticsOverview(orgUuid, agentId), getAgentSessionAnalyticsTimeseries(orgUuid, agentId, groupBy)])
      .then(([overviewPayload, timeseriesPayload]) => {
        if (!active) {
          return;
        }
        setOverview(overviewPayload);
        setTimeseries(timeseriesPayload);
        setLoadError(null);
        setLoading(false);
      })
      .catch((error: unknown) => {
        if (!active) {
          return;
        }
        setOverview(emptyAgentSessionAnalyticsOverview());
        setTimeseries({ data: [] });
        setLoadError(errorMessage(error));
        setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [agentId, groupBy, orgUuid]);

  const data = overview ?? emptyAgentSessionAnalyticsOverview();
  const timeRows = timeseries?.data ?? timeseries?.data_points ?? [];
  const hasTimeseries = timeRows.length > 0;
  const toolCounts = data.tool_call_counts ?? {};
  const stopReasonCounts = data.stop_reason_counts ?? {};

  return (
    <div className="space-y-5">
      {loadError ? (
        <AgentDetailErrorAlert className="max-w-xl">
          {msg('managedAgents.observability.loadError', "Couldn't load usage analytics.")} {loadError}
        </AgentDetailErrorAlert>
      ) : null}

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <AgentMetricCard title={msg('managedAgents.observability.sessions', 'Sessions')} value={loading ? '...' : formatInteger(metricValue(data.sessions_count))} />
        <AgentMetricCard title={msg('managedAgents.observability.errorRate', 'Error rate')} value={loading ? '...' : formatPercent(metricValue(data.error_rate))} />
        <AgentMetricCard title={msg('managedAgents.observability.totalInputTokens', 'Total input tokens')} value={loading ? '...' : formatInteger(metricTotal(data.input_tokens))} />
        <AgentMetricCard title={msg('managedAgents.observability.totalOutputTokens', 'Total output tokens')} value={loading ? '...' : formatInteger(metricTotal(data.output_tokens))} />
      </div>

      <Card className="gap-0 py-0">
        <CardHeader className="flex flex-wrap items-start justify-between gap-3 border-b border-border py-3">
          <div>
            <CardTitle>{msg('managedAgents.observability.sessionActivity', 'Session activity')}</CardTitle>
            {data.data_as_of ? (
              <CardDescription className="mt-1 text-xs">{msg('managedAgents.observability.dataAsOf', 'Data as of {date}', { date: formatDetailDate(data.data_as_of) })}</CardDescription>
            ) : null}
          </div>
          <Select<string>
            value={groupBy}
            items={groupByOptions}
            onValueChange={(nextValue) => {
              if (nextValue !== null) {
                setGroupBy(nextValue);
              }
            }}
          >
          <SelectTrigger
            aria-label={msg('managedAgents.observability.groupBy', 'Group by')}
            className="h-9 border-border px-3 text-sm text-foreground"
          >
            <span className="text-muted-foreground">{msg('managedAgents.observability.groupBy', 'Group by')}</span>
            <SelectValue>{selectedGroupBy.label}</SelectValue>
          </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {groupByOptions.map((option) => (
                <SelectItem key={option.value} value={option.value} label={option.label}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </CardHeader>
        <CardContent className="flex h-[260px] items-center justify-center text-sm text-muted-foreground">
          {loading ? (
            msg('managedAgents.observability.loading', 'Loading analytics...')
          ) : hasTimeseries ? (
            <AgentTimeseriesPreview rows={timeRows} groupBy={groupBy} />
          ) : (
            msg('managedAgents.observability.noSessionActivity', 'No session activity in this range')
          )}
        </CardContent>
      </Card>

      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <AgentQuantileCard title={msg('managedAgents.observability.turns', 'Turns')} metric={data.turns_per_session} suffix={msg('managedAgents.observability.perSession', 'per session')} formatValue={formatDecimal} />
        <AgentQuantileCard title={msg('managedAgents.observability.activeTime', 'Active time')} metric={data.active_time} suffix="" formatValue={formatDurationSeconds} />
        <AgentQuantileCard title={msg('managedAgents.observability.inputTokens', 'Input tokens')} metric={data.input_tokens_per_session} suffix={msg('managedAgents.observability.perSession', 'per session')} formatValue={formatInteger} />
        <AgentQuantileCard title={msg('managedAgents.observability.outputTokens', 'Output tokens')} metric={data.output_tokens_per_session} suffix={msg('managedAgents.observability.perSession', 'per session')} formatValue={formatInteger} />
      </div>

      <div className="grid gap-5 lg:grid-cols-2">
        <AgentAnalyticsBreakdown title={msg('managedAgents.observability.toolUsage', 'Tool usage')} values={toolCounts} emptyLabel={msg('managedAgents.observability.noToolUsage', 'No tool calls in this range')} />
        <AgentAnalyticsBreakdown title={msg('managedAgents.observability.stopReasons', 'Stop reasons')} values={stopReasonCounts} emptyLabel={msg('managedAgents.observability.noStopReasons', 'No stop reasons in this range')} />
      </div>
    </div>
  );
}

function AgentDetailErrorAlert({
  title,
  className,
  children
}: {
  title?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Alert variant="destructive" className={className}>
      <AlertCircle className="mt-0.5 size-4 shrink-0" aria-hidden />
      {title ? <AlertTitle>{title}</AlertTitle> : null}
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  );
}

export function AgentEditDialog({
  agent,
  workspaceId,
  onClose,
  onSaved
}: {
  agent: AgentApiResponse;
  workspaceId: string;
  onClose: () => void;
  onSaved: (agent: AgentApiResponse) => void;
}) {
  const { msg } = useI18n();
  const initialConfig = useMemo(() => agentEditConfig(agent), [agent]);
  const [baselineVersion] = useState(() => agent.version);
  const [format, setFormat] = useState<CodeFormat>('YAML');
  const [configText, setConfigText] = useState(() => agentEditConfigText(initialConfig, 'YAML'));
  const [configError, setConfigError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const validateEditorText = useCallback((text: string, nextFormat: CodeFormat) => {
    const parsed = parseAgentEditConfigText(text, nextFormat);
    return parsed.ok ? null : parsed.error;
  }, []);

  const parseCurrentConfig = useCallback(() => {
    const parsed = parseAgentEditConfigText(configText, format);
    if (!parsed.ok) {
      setConfigError(parsed.error);
      return null;
    }
    setConfigError(null);
    return parsed.config;
  }, [configText, format]);

  const handleEditorChange = useCallback((value: string) => {
    setConfigText(value);
    setSaveError(null);
    const parsed = parseAgentEditConfigText(value, format);
    if (!parsed.ok) {
      setConfigError(parsed.error);
      return;
    }
    setConfigError(null);
  }, [format]);

  const selectFormat = (nextFormat: CodeFormat) => {
    if (nextFormat === format) {
      return;
    }
    const parsed = parseCurrentConfig();
    if (!parsed) {
      return;
    }
    setFormat(nextFormat);
    setConfigText(agentEditConfigText(parsed, nextFormat));
    setSaveError(null);
  };

  const submit = useCallback(async () => {
    if (submitting) {
      return;
    }
    const parsed = parseCurrentConfig();
    if (!parsed) {
      return;
    }

    setSubmitting(true);
    setSaveError(null);
    try {
      const updated = await updateAgentDetail(agent.id, buildAgentUpdateInput(baselineVersion, parsed), workspaceId);
      onSaved(updated);
    } catch (submitError) {
      setSaveError(agentEditSaveErrorMessage(submitError));
      setSubmitting(false);
    }
  }, [agent.id, baselineVersion, onSaved, parseCurrentConfig, submitting, workspaceId]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 's') {
        event.preventDefault();
        void submit();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [submit]);

  const displayedError = configError ?? saveError;
  const saveDisabled = submitting || Boolean(configError);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        aria-modal="true"
        aria-label={msg('managedAgents.agents.editDialog.title', 'Edit agent')}
        className="h-[min(760px,calc(100dvh-2rem))] max-w-[1120px] overflow-hidden rounded-[18px] bg-popover p-0 shadow-xl sm:max-w-[1120px]"
        showCloseButton={false}
      >
        <div className="flex h-full min-h-0 flex-col px-8 pb-8 pt-7 text-foreground">
          <DialogClose
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon-lg"
                className="absolute right-8 top-8 text-foreground hover:bg-accent"
              />
            }
          >
            <X className="size-7" aria-hidden />
            <span className="sr-only">{msg('common.close', 'Close')}</span>
          </DialogClose>

          <DialogHeader className="pr-12">
            <DialogTitle className="text-[32px] font-semibold leading-10 text-foreground">
              {msg('managedAgents.agents.editDialog.title', 'Edit agent')}
            </DialogTitle>
          </DialogHeader>

          <Card className="mt-7 flex min-h-0 flex-1 gap-0 overflow-hidden py-0">
            <CardHeader className="flex h-12 shrink-0 flex-row items-center justify-between gap-3 border-b border-border px-5 py-0">
              <FormatSelect
                value={format}
                onChange={selectFormat}
                align="left"
                buttonClassName="bg-accent px-3 text-muted-foreground hover:text-foreground"
                menuClassName="z-[120] w-40 rounded-[14px] bg-popover p-2"
              />
              <CopyButton value={configText} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
            </CardHeader>
            <CardContent className="min-h-0 flex-1 overflow-hidden p-0">
              <AgentConfigEditor
                id="edit-agent-config-editor"
                value={configText}
                format={format}
                onChange={handleEditorChange}
                ariaLabel={msg('managedAgents.agents.editDialog.configLabel', 'Agent configuration')}
                lineNumbers
                validate={validateEditorText}
              />
            </CardContent>
          </Card>

          {displayedError ? <p className="mt-3 text-sm leading-5 text-destructive">{displayedError}</p> : null}

          <div className="mt-6 flex justify-end">
            <Button
              type="button"
              disabled={saveDisabled}
              size="lg"
              className="h-11 px-5 text-[16px] leading-6 disabled:cursor-not-allowed disabled:bg-muted disabled:text-muted-foreground/70"
              onClick={() => void submit()}
            >
              {submitting ? msg('common.saving', 'Saving...') : msg('managedAgents.agents.editDialog.saveNewVersion', 'Save new version')}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function AgentVersionDropdown({
  label,
  versions,
  activeVersion,
  latestVersion,
  onSelect
}: {
  label: string;
  versions: AgentApiResponse[];
  activeVersion: number;
  latestVersion: number;
  onSelect: (version: number) => void;
}) {
  const options = uniqueVersionNumbers(versions, latestVersion);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="lg"
            className="gap-2 text-sm font-medium text-foreground"
          />
        }
      >
        {label}
        <ChevronDown className="size-4 text-muted-foreground/70" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={8} className="w-[190px]">
        <DropdownMenuRadioGroup value={String(activeVersion)} onValueChange={(version) => onSelect(Number(version))}>
          {options.map((version) => (
            <DropdownMenuRadioItem
              key={version}
              value={String(version)}
              className="h-9 pl-3 pr-8 text-sm"
            >
              v{version}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function AgentVersionFilterDropdown({
  versions,
  value,
  onSelect
}: {
  versions: AgentApiResponse[];
  value: AgentDetailVersionFilter;
  onSelect: (value: AgentDetailVersionFilter) => void;
}) {
  const { msg } = useI18n();
  const latest = latestAgentVersion(versions, null);
  const options = uniqueVersionNumbers(versions, latest);
  const valueLabel = value ? `v${value}` : msg('common.all', 'All');

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="lg"
            className="gap-2 text-sm text-muted-foreground"
          />
        }
      >
        <span>{msg('managedAgents.agents.detail.version', 'Version')}</span>
        <span className="font-medium text-foreground">{valueLabel}</span>
        <ChevronDown className="size-4 text-muted-foreground/70" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={8} className="w-[180px]">
        <DropdownMenuRadioGroup value={value === null ? 'all' : String(value)} onValueChange={(nextValue) => onSelect(nextValue === 'all' ? null : Number(nextValue))}>
          <DropdownMenuRadioItem value="all" className="h-9 pl-3 pr-8 text-sm">
            {msg('common.all', 'All')}
          </DropdownMenuRadioItem>
          {options.map((version) => (
            <DropdownMenuRadioItem
              key={version}
              value={String(version)}
              className="h-9 pl-3 pr-8 text-sm"
            >
              v{version}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function AgentDetailCreatedFilterDropdown({
  value,
  onSelect
}: {
  value: AgentDetailCreatedFilter;
  onSelect: (value: AgentDetailCreatedFilter) => void;
}) {
  const { msg } = useI18n();
  const options: Array<{ value: AgentDetailCreatedFilter; label: string }> = [
    { value: 'all_time', label: msg('managedAgents.filters.allTime', 'All time') },
    { value: 'today', label: msg('managedAgents.filters.today', 'Today') },
    { value: 'last_hour', label: msg('managedAgents.filters.lastHour', 'Last hour') },
    { value: 'last_day', label: msg('managedAgents.filters.lastDay', 'Last day') },
    { value: 'last_7_days', label: msg('managedAgents.filters.last7Days', 'Last 7 days') },
    { value: 'last_30_days', label: msg('managedAgents.filters.last30Days', 'Last 30 days') }
  ];
  const label = options.find((option) => option.value === value)?.label ?? msg('managedAgents.filters.allTime', 'All time');

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="lg"
            className="gap-2 text-sm text-muted-foreground"
          />
        }
      >
        <span>{msg('managedAgents.filters.created', 'Created')}</span>
        <span className="font-medium text-foreground">{label}</span>
        <ChevronDown className="size-4 text-muted-foreground/70" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={8} className="w-[220px]">
        <DropdownMenuRadioGroup value={value} onValueChange={(nextValue) => onSelect(nextValue as AgentDetailCreatedFilter)}>
          {options.map((option) => (
            <DropdownMenuRadioItem
              key={option.value}
              value={option.value}
              className="h-9 pl-3 pr-8 text-sm"
            >
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function AgentDeploymentFilterDropdown({
  deployments,
  value,
  loading,
  onSelect
}: {
  deployments: DeploymentApiResponse[];
  value: string;
  loading: boolean;
  onSelect: (value: string) => void;
}) {
  const { msg } = useI18n();
  const selected = deployments.find((deployment) => deployment.id === value);
  const valueLabel = value ? selected?.name || compactEntityId(value) : msg('common.all', 'All');

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="lg"
            className="gap-2 text-sm text-muted-foreground"
          />
        }
      >
        <span>{msg('managedAgents.deployments.kind', 'Deployment')}</span>
        <span className="max-w-[180px] truncate font-medium text-foreground">{loading ? msg('common.loading', 'Loading...') : valueLabel}</span>
        <ChevronDown className="size-4 text-muted-foreground/70" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={8} className="w-[260px]">
        <DropdownMenuRadioGroup value={value || 'all'} onValueChange={(nextValue) => onSelect(nextValue === 'all' ? '' : nextValue)}>
          <DropdownMenuRadioItem value="all" className="h-9 pl-3 pr-8 text-sm">
            {msg('common.all', 'All')}
          </DropdownMenuRadioItem>
          {deployments.map((deployment) => (
            <DropdownMenuRadioItem
              key={deployment.id}
              value={deployment.id}
              className="h-9 pl-3 pr-8 text-sm"
            >
              <span className="min-w-0 truncate">{deployment.name || deployment.id}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        {!deployments.length && !loading ? (
          <div className="px-3 py-2 text-sm text-muted-foreground">{msg('managedAgents.deployments.noDeployments', 'No deployments')}</div>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function AgentStatusFilterDropdown({
  value,
  onSelect
}: {
  value: AgentDetailStatusFilter;
  onSelect: (value: AgentDetailStatusFilter) => void;
}) {
  const { msg } = useI18n();
  const options: Array<{ value: AgentDetailStatusFilter; label: string }> = [
    { value: 'all', label: msg('common.all', 'All') },
    { value: 'active', label: msg('managedAgents.sessions.statusActive', 'Active') },
    { value: 'running', label: msg('managedAgents.sessions.statusRunning', 'Running') },
    { value: 'idle', label: msg('managedAgents.sessions.statusIdle', 'Idle') },
    { value: 'rescheduling', label: msg('managedAgents.sessions.statusRescheduling', 'Rescheduling') },
    { value: 'terminated', label: msg('managedAgents.sessions.statusTerminated', 'Terminated') }
  ];
  const label = options.find((option) => option.value === value)?.label ?? msg('common.all', 'All');

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="lg"
            className="gap-2 text-sm text-muted-foreground"
          />
        }
      >
        <span>{managedColumnLabel('Status', msg)}</span>
        <span className="font-medium text-foreground">{label}</span>
        <ChevronDown className="size-4 text-muted-foreground/70" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={8} className="w-[200px]">
        <DropdownMenuRadioGroup value={value} onValueChange={(nextValue) => onSelect(nextValue as AgentDetailStatusFilter)}>
          {options.map((option) => (
            <DropdownMenuRadioItem
              key={option.value}
              value={option.value}
              className="h-9 pl-3 pr-8 text-sm"
            >
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function AgentMetricCard({ title, value }: { title: string; value: ReactNode }) {
  return (
    <Card className="gap-0 py-0">
      <CardContent className="py-3">
        <div className="text-xs font-medium text-muted-foreground">{title}</div>
        <div className="mt-2 text-2xl font-semibold leading-8 text-foreground">{value}</div>
      </CardContent>
    </Card>
  );
}

export function AgentQuantileCard({
  title,
  metric,
  suffix,
  formatValue
}: {
  title: string;
  metric?: AnalyticsMetricBucket;
  suffix: string;
  formatValue: (value: number) => string;
}) {
  const quantileOptions = ['p50', 'p90', 'p95'] as const;
  const [quantile, setQuantile] = useState<'p50' | 'p90' | 'p95'>('p50');

  return (
    <Card className="gap-0 py-0">
      <CardContent className="py-3">
        <Tabs value={quantile} onValueChange={(nextValue) => setQuantile(nextValue as 'p50' | 'p90' | 'p95')} className="gap-0">
          <div className="flex items-center justify-between gap-3">
            <div className="text-base font-semibold text-foreground">{title}</div>
            <TabsList aria-label={title} className="h-8 rounded-lg p-0.5">
              {quantileOptions.map((option) => (
                <TabsTrigger key={option} value={option} className="h-7 rounded-md px-2 text-xs">
                  {option}
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
          {quantileOptions.map((option) => {
            const value = metricQuantile(metric, option);
            return (
              <TabsContent key={option} value={option} className="mt-8">
                <div className="text-2xl font-semibold leading-8 text-foreground">{value ? formatValue(value) : '-'}</div>
                {suffix ? <div className="mt-1 text-sm text-muted-foreground">{suffix}</div> : null}
              </TabsContent>
            );
          })}
        </Tabs>
      </CardContent>
    </Card>
  );
}

export function AgentTimeseriesPreview({ rows, groupBy }: { rows: Array<Record<string, unknown>>; groupBy: string }) {
  const maxValue = Math.max(1, ...rows.map((row) => numericValueFromKeys(row, ['sessions_count', 'count', 'value'])));
  return (
    <div className="flex h-full w-full max-w-[720px] items-end justify-center gap-2 px-4 pb-4">
      {rows.slice(-24).map((row, index) => {
        const value = numericValueFromKeys(row, ['sessions_count', 'count', 'value']);
        const height = Math.max(8, Math.round((value / maxValue) * 190));
        const label = stringValueFromKeys(row, ['outcome_category', 'agent_version', 'date', 'time_bucket']) || `${groupBy} ${index + 1}`;
        return (
          <div key={`${label}-${index}`} className="flex min-w-0 flex-1 flex-col items-center gap-2">
            <div className="w-full rounded-t bg-accent" style={{ height }} title={`${label}: ${value}`} />
            <span className="max-w-full truncate text-[10px] text-muted-foreground/70">{value}</span>
          </div>
        );
      })}
    </div>
  );
}

export function AgentAnalyticsBreakdown({ title, values, emptyLabel }: { title: string; values: Record<string, unknown>; emptyLabel: string }) {
  const entries = Object.entries(values)
    .map(([label, value]) => [label, metricValue(typeof value === 'number' ? value : objectRecord(value))] as const)
    .filter(([, value]) => Number.isFinite(value) && value > 0);
  const total = entries.reduce((sum, [, value]) => sum + value, 0);
  return (
    <Card className="gap-0 py-0">
      <CardHeader className="border-b border-border py-3">
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 py-4">
        {entries.length ? (
          entries.map(([label, value]) => {
            const pct = total ? (value / total) * 100 : 0;
            return (
              <div key={label}>
                <div className="mb-1 flex items-center justify-between gap-3 text-sm">
                  <span className="truncate text-foreground">{titleCase(label.replace(/_/g, ' '))}</span>
                  <span className="text-muted-foreground">{formatInteger(value)}</span>
                </div>
                <div className="h-2 rounded-full bg-secondary">
                  <div className="h-2 rounded-full bg-accent" style={{ width: `${Math.max(2, pct)}%` }} />
                </div>
              </div>
            );
          })
        ) : (
          <div className="py-8 text-center text-sm text-muted-foreground">{emptyLabel}</div>
        )}
      </CardContent>
    </Card>
  );
}
