import { useEffect, useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Badge } from '../../../shared/ui/badge';
import { CardContent, CardDescription, CardHeader, CardTitle } from '../../../shared/ui/card';
import { TabsContent } from '../../../shared/ui/tabs';
import { listVaultCredentials, retrieveAgent, retrieveManagedEntity } from '../api';
import { agentModelName } from '../agents/model';
import { environmentPackageRows, credentialAuthLabel } from '../resources/model';
import {
  type AgentApiResponse,
  type EnvironmentApiResponse,
  type SessionApiResponse,
  type SessionResourceApiResponse,
  type VaultApiResponse,
  type VaultCredentialApiResponse,
} from '../types';
import { errorMessage, objectRecord, titleCase } from '../utils';
import { SessionWorkspaceCard } from './SessionWorkspaceCard';

type VaultWithCredentials = { vault: VaultApiResponse; items: VaultCredentialApiResponse[] };

type RelatedEntities = {
  agent: AgentApiResponse | null;
  environment: EnvironmentApiResponse | null;
  credentials: VaultWithCredentials[];
  loading: boolean;
  error: string | null;
};

const emptyRelatedEntities: RelatedEntities = {
  agent: null,
  environment: null,
  credentials: [],
  loading: true,
  error: null,
};

export function SessionEntityPanels({
  refreshKey,
  resources,
  session,
  workspaceId,
}: {
  refreshKey: number;
  resources: SessionResourceApiResponse[];
  session: SessionApiResponse;
  workspaceId: string;
}) {
  const agentReference = objectRecord(session.agent);
  const agentId = typeof agentReference.id === 'string' ? agentReference.id : '';
  const agentVersion = typeof agentReference.version === 'number' ? agentReference.version : null;
  const vaultKey = (Array.isArray(session.vault_ids) ? session.vault_ids.filter(isNonEmptyString) : []).join('\0');
  const related = useRelatedEntities(agentId, agentVersion, session.environment_id, vaultKey, workspaceId, refreshKey);
  const { msg } = useI18n();
  const emptyText = related.loading
    ? msg('common.loading', 'Loading...')
    : related.error || msg('managedAgents.sessions.context.unavailable', 'Details unavailable');

  return (
    <>
      <TabsContent value="resources" className="subtle-scrollbar-auto mt-0 min-h-0 overflow-y-auto py-4">
        <FullWidthCard
          title={msg('managedAgents.sessions.context.resourcesTitle', 'Mounted resources')}
          description={msg(
            'managedAgents.sessions.context.resourcesDescription',
            'Files, repositories, and memory stores available inside this session.',
          )}
        >
          {resources.length ? (
            <ResourceGrid resources={resources} />
          ) : (
            <EmptyText>{msg('managedAgents.sessions.nested.noResources', 'No resources mounted')}</EmptyText>
          )}
        </FullWidthCard>
      </TabsContent>
      <AgentDetails agent={related.agent} emptyText={emptyText} />
      <EnvironmentDetails environment={related.environment} emptyText={emptyText} />
      <CredentialDetails credentials={related.credentials} emptyText={emptyText} hasVaults={Boolean(vaultKey)} />
    </>
  );
}

function useRelatedEntities(
  agentId: string,
  agentVersion: number | null,
  environmentId: string,
  vaultKey: string,
  workspaceId: string,
  refreshKey: number,
) {
  const [related, setRelated] = useState<RelatedEntities>(emptyRelatedEntities);
  useEffect(() => {
    let active = true;
    setRelated(emptyRelatedEntities);
    const vaultIds = vaultKey ? vaultKey.split('\0') : [];
    void Promise.allSettled([
      agentId ? retrieveAgent(agentId, workspaceId, agentVersion) : Promise.resolve(null),
      environmentId ? retrieveManagedEntity('environments', environmentId, workspaceId) : Promise.resolve(null),
      // Per-vault allSettled: one failing vault must not blank out the others
      // (design doc: keep the rest of a category when one entity fails).
      Promise.allSettled(
        vaultIds.map(async (vaultId): Promise<VaultWithCredentials> => ({
          vault: (await retrieveManagedEntity('credential-vaults', vaultId, workspaceId)) as VaultApiResponse,
          items: (await listVaultCredentials(vaultId, workspaceId)).data ?? [],
        })),
      ),
    ]).then((results) => {
      if (!active) return;
      const vaultResults = results[2].status === 'fulfilled' ? results[2].value : [];
      const rejected = [...results.slice(0, 2), ...vaultResults].find(
        (result): result is PromiseRejectedResult => result.status === 'rejected',
      );
      setRelated({
        agent: results[0].status === 'fulfilled' ? (results[0].value as AgentApiResponse | null) : null,
        environment: results[1].status === 'fulfilled' ? (results[1].value as EnvironmentApiResponse | null) : null,
        credentials: vaultResults
          .filter((result): result is PromiseFulfilledResult<VaultWithCredentials> => result.status === 'fulfilled')
          .map((result) => result.value),
        loading: false,
        error: rejected ? errorMessage(rejected.reason) : null,
      });
    });
    return () => {
      active = false;
    };
  }, [agentId, agentVersion, environmentId, refreshKey, vaultKey, workspaceId]);
  return related;
}

function AgentDetails({ agent, emptyText }: { agent: AgentApiResponse | null; emptyText: string }) {
  const { msg } = useI18n();
  return (
    <TabsContent value="agent" className="subtle-scrollbar-auto mt-0 min-h-0 overflow-y-auto py-4">
      <FullWidthCard
        title={agent?.name || msg('managedAgents.sessions.detail.agentTab', 'Agent')}
        description={agent?.description || emptyText}
      >
        {agent ? (
          <div className="space-y-6">
            <DetailGrid
              rows={[
                [msg('common.name', 'Name'), agent.name],
                [msg('analytics.table.model', 'Model'), agentModelName(agent.model)],
                [msg('managedAgents.agents.detail.version', 'Version'), `v${agent.version}`],
                [msg('common.id', 'ID'), agent.id],
                [msg('common.created', 'Created'), agent.created_at],
                [msg('managedAgents.common.updatedAt', 'Updated at'), agent.updated_at],
              ]}
            />
            <section>
              <h3 className="mb-2 text-sm font-medium">
                {msg('managedAgents.agents.detail.systemPrompt', 'System prompt')}
              </h3>
              <pre className="max-h-80 overflow-auto rounded-lg border border-border bg-muted/50 p-4 font-sans text-sm leading-6 whitespace-pre-wrap">
                {agent.system || msg('managedAgents.agents.detail.noSystemPrompt', 'No system prompt configured.')}
              </pre>
            </section>
            <section>
              <h3 className="mb-2 text-sm font-medium">
                {msg('managedAgents.agents.detail.mcpsAndTools', 'MCPs and tools')}
              </h3>
              <div className="flex flex-wrap gap-2">
                {[...agent.tools, ...agent.mcp_servers].map((tool, index) => {
                  const record = objectRecord(tool);
                  return (
                    <Badge key={String(record.name ?? record.type ?? index)} variant="outline">
                      {String(record.name ?? record.type ?? 'tool')}
                    </Badge>
                  );
                })}
              </div>
            </section>
          </div>
        ) : null}
      </FullWidthCard>
    </TabsContent>
  );
}

function EnvironmentDetails({
  environment,
  emptyText,
}: {
  environment: EnvironmentApiResponse | null;
  emptyText: string;
}) {
  const { msg } = useI18n();
  const config = objectRecord(environment?.config);
  const networking = objectRecord(config.networking);
  const packages = environmentPackageRows(config.packages);
  const metadata = objectRecord((environment as (EnvironmentApiResponse & { metadata?: unknown }) | null)?.metadata);
  return (
    <TabsContent value="environment" className="subtle-scrollbar-auto mt-0 min-h-0 overflow-y-auto py-4">
      <FullWidthCard
        title={environment?.name || msg('managedAgents.sessions.detail.environmentTab', 'Environment')}
        description={environment?.description || emptyText}
      >
        {environment ? (
          <div className="space-y-6">
            <DetailGrid
              rows={[
                [msg('common.id', 'ID'), environment.id],
                [msg('common.status', 'Status'), titleCase(environment.state)],
                [msg('managedAgents.environments.overview.scope', 'Scope'), environment.scope],
                [
                  msg('managedAgents.environments.networking.title', 'Networking'),
                  titleCase(String(networking.type ?? 'unrestricted')),
                ],
                [msg('common.created', 'Created'), environment.created_at],
                [msg('managedAgents.common.updatedAt', 'Updated at'), environment.updated_at],
              ]}
            />
            <DetailSection
              title={msg('managedAgents.environments.packages.title', 'Packages')}
              empty={!packages.length}
            >
              {packages.map((item) => (
                <Badge key={`${item.manager}:${item.value}`} variant="outline">
                  {item.manager}: {item.value}
                </Badge>
              ))}
            </DetailSection>
            <DetailSection
              title={msg('managedAgents.environments.metadata.title', 'Metadata')}
              empty={!Object.keys(metadata).length}
            >
              {Object.entries(metadata).map(([key, value]) => (
                <Badge key={key} variant="secondary">
                  {key}: {String(value)}
                </Badge>
              ))}
            </DetailSection>
          </div>
        ) : null}
      </FullWidthCard>
    </TabsContent>
  );
}

function CredentialDetails({
  credentials,
  emptyText,
  hasVaults,
}: {
  credentials: RelatedEntities['credentials'];
  emptyText: string;
  hasVaults: boolean;
}) {
  const { msg } = useI18n();
  return (
    <TabsContent value="vaults" className="subtle-scrollbar-auto mt-0 min-h-0 overflow-y-auto py-4">
      <FullWidthCard
        title={msg('managedAgents.sessions.context.vaultsTitle', 'Session credentials')}
        description={msg(
          'managedAgents.sessions.context.vaultsDescription',
          'Credentials available to this session. Secret values are never shown here.',
        )}
      >
        {credentials.length ? (
          credentials.map(({ vault, items }) => (
            <section key={vault.id} className="mb-6 last:mb-0">
              <DetailGrid
                rows={[
                  [msg('managedAgents.credentialVaults.kindTitle', 'Vault'), vault.display_name],
                  [msg('common.id', 'ID'), vault.id],
                  [msg('common.created', 'Created'), vault.created_at],
                ]}
              />
              <div className="mt-4 divide-y divide-border rounded-lg border border-border">
                {items.length ? (
                  items.map((credential) => (
                    <div
                      key={credential.id}
                      className="grid gap-3 px-4 py-3 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]"
                    >
                      <div>
                        <div className="font-medium">{credential.display_name}</div>
                        <div className="font-mono text-xs text-muted-foreground">{credential.id}</div>
                      </div>
                      <div className="text-sm text-muted-foreground">{credentialAuthLabel(credential.auth, msg)}</div>
                      <div className="text-xs text-muted-foreground">{credential.updated_at}</div>
                    </div>
                  ))
                ) : (
                  <EmptyText>{msg('managedAgents.credentialVaults.credentials.empty', 'No credentials yet')}</EmptyText>
                )}
              </div>
            </section>
          ))
        ) : (
          <EmptyText>
            {hasVaults ? emptyText : msg('managedAgents.sessions.context.noVaults', 'No credentials connected')}
          </EmptyText>
        )}
      </FullWidthCard>
    </TabsContent>
  );
}

function FullWidthCard({
  children,
  description,
  title,
}: {
  children: React.ReactNode;
  description: string;
  title: string;
}) {
  return (
    <SessionWorkspaceCard>
      <CardHeader className="border-b border-border">
        <CardTitle>{title}</CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </SessionWorkspaceCard>
  );
}

function DetailGrid({ rows }: { rows: Array<[string, string]> }) {
  return (
    <dl className="grid gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-2 xl:grid-cols-3">
      {rows.map(([label, value]) => (
        <div key={label} className="min-w-0 bg-card px-4 py-3">
          <dt className="text-xs font-medium uppercase text-muted-foreground">{label}</dt>
          <dd className="mt-1 truncate text-sm" title={value}>
            {value || '—'}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function DetailSection({ children, empty, title }: { children: React.ReactNode; empty: boolean; title: string }) {
  return (
    <section>
      <h3 className="mb-2 text-sm font-medium">{title}</h3>
      <div className="flex flex-wrap gap-2">
        {empty ? <span className="text-sm text-muted-foreground">—</span> : children}
      </div>
    </section>
  );
}

function ResourceGrid({ resources }: { resources: SessionResourceApiResponse[] }) {
  return (
    <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      {resources.map((resource, index) => (
        <div key={String(resource.id ?? index)} className="rounded-lg border border-border p-4">
          <div className="font-medium">{resourceName(resource)}</div>
          <div className="mt-1 font-mono text-xs text-muted-foreground">{String(resource.id ?? '—')}</div>
          <Badge variant="secondary" className="mt-3">
            {titleCase(String(resource.type ?? 'resource').replaceAll('_', ' '))}
          </Badge>
        </div>
      ))}
    </div>
  );
}

function EmptyText({ children }: { children: React.ReactNode }) {
  return <div className="py-12 text-center text-sm text-muted-foreground">{children}</div>;
}

function resourceName(resource: SessionResourceApiResponse) {
  for (const key of ['filename', 'name', 'path', 'repository', 'url'])
    if (typeof resource[key] === 'string' && resource[key]) return resource[key];
  return String(resource.id ?? 'Resource');
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === 'string' && Boolean(value);
}
