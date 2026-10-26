import type { APIBackend } from './api';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation } from '@tanstack/react-router';
import { AlertCircle, Bot, Plus } from 'lucide-react';
import type { ApiError } from '../../shared/api/client';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { canManageLLMProviders } from '../../shared/permissions/llm-providers';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath } from '../../shared/workspaces/presentation';
import { AccessDenied } from '../../shared/ui/access-denied';
import { Alert, AlertDescription } from '../../shared/ui/alert';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { Empty, EmptyContent, EmptyHeader, EmptyMedia, EmptyTitle } from '../../shared/ui/empty';
import { Skeleton } from '../../shared/ui/skeleton';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { toast } from '../../shared/ui/sonner';
import {
  deleteOrganizationLLMProvider,
  getOrganizationLLMProvider,
  persistLLMProvider,
  previewOrganizationProviderModels,
  previewSavedProviderModels,
  syncOrganizationLLMProvider,
  upsertOrganizationLLMProvider,
  deleteLLMProvider,
  listLLMProviders,
  listProviderPresets,
  previewProviderModels,
  syncProviderModels,
  updateLLMProvider,
  type LLMProvider,
  type LLMProviderInput,
} from './api';
import {
  ALL_PROVIDERS_ID,
  catalogModels,
  providerInput,
  readableError,
  resolvedProviderSelection,
  type CatalogModel,
} from './catalog';
import { ModelCatalog } from './ModelCatalog';
import { ProviderDeleteDialog } from './ProviderDeleteDialog';
import { ProviderFormDialog } from './ProviderFormDialog';

type ProviderScope = 'workspace' | 'organization';

export function LLMModelsPage({ scope = 'workspace' }: { scope?: ProviderScope }) {
  const location = useLocation();
  const routeWorkspaceId = workspaceIdFromPath(location.pathname);
  const { account, csrfToken } = useAuth();
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { orgUuid, activeWorkspaceId, selectWorkspace } = useWorkspace();
  const canManage = canManageLLMProviders(account, orgUuid);
  const workspaceId = routeWorkspaceId ?? activeWorkspaceId;
  const [editing, setEditing] = useState<LLMProvider | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [formError, setFormError] = useState('');
  const refreshAfterSaveRef = useRef(false);
  const [deleting, setDeleting] = useState<LLMProvider | null>(null);
  const [query, setQuery] = useState('');
  const [selectedProviderId, setSelectedProviderId] = useState(ALL_PROVIDERS_ID);
  const [isSyncing, setIsSyncing] = useState(false);
  const providersKey = useMemo(
    () => ['console', 'llm-providers', orgUuid, workspaceId] as const,
    [orgUuid, workspaceId],
  );

  const organizationKey = useMemo(() => ['console', 'llm-providers', 'organization', orgUuid] as const, [orgUuid]);

  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);

  const providersQuery = useQuery({
    queryKey: providersKey,
    queryFn: () => listLLMProviders(orgUuid ?? '', workspaceId),
    enabled: scope === 'workspace' && Boolean(orgUuid && workspaceId),
    retry: false,
  });
  const organizationQuery = useQuery({
    queryKey: organizationKey,
    queryFn: () => getOrganizationLLMProvider(orgUuid ?? ''),
    enabled: Boolean(orgUuid),
    retry: false,
  });
  const presetsKey = ['console', 'llm-provider-presets', orgUuid];
  const presetsQuery = useQuery({
    queryKey: presetsKey,
    queryFn: () => listProviderPresets(orgUuid ?? ''),
    enabled: canManage && formOpen && !editing,
    retry: false,
  });
  const organizationProvider = organizationQuery.data;
  const providers = [
    ...(scope === 'workspace' ? (providersQuery.data ?? []) : []),
    ...(organizationProvider ? [organizationProvider] : []),
  ];
  const providersError = scope === 'organization' ? organizationQuery.error : providersQuery.error;
  const catalogAccessDenied = isCatalogAccessDenied(providersError);
  const activeProviderId = resolvedProviderSelection(providers, selectedProviderId);
  const catalog = catalogModels(providers);

  const invalidateCatalog = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: providersKey }),
      queryClient.invalidateQueries({ queryKey: organizationKey }),
    ]);
  };

  const discoverModels = useCallback(
    async (baseUrl: string, apiKey: string, apiBackend: APIBackend, provider?: LLMProvider) => {
      if (!orgUuid) {
        throw new Error(msg('llmModels.noOrganization', 'No organization is available.'));
      }
      if (!apiKey && provider)
        return (await previewSavedProviderModels(orgUuid, workspaceId, provider, csrfToken)).models.map(
          (model) => model.id,
        );
      const input = { base_url: baseUrl, api_key: apiKey, api_backend: apiBackend };
      const preview =
        scope === 'organization'
          ? await previewOrganizationProviderModels(orgUuid, input, csrfToken)
          : await previewProviderModels(orgUuid, workspaceId, input, csrfToken);
      return preview.models.map((model) => model.id);
    },
    [csrfToken, msg, orgUuid, scope, workspaceId],
  );

  const syncLiveModels = async (targets: LLMProvider[]) => {
    if (!orgUuid) {
      throw new Error(msg('llmModels.noOrganization', 'No organization is available.'));
    }
    if (targets.length === 0) {
      return;
    }
    setIsSyncing(true);
    try {
      const results = await Promise.allSettled(
        targets.map((provider) =>
          scope === 'organization'
            ? syncOrganizationLLMProvider(orgUuid, csrfToken)
            : syncProviderModels(orgUuid, workspaceId, provider.id, csrfToken),
        ),
      );
      const failed = results.find((result) => result.status === 'rejected');
      if (failed && failed.status === 'rejected') {
        throw failed.reason;
      }
    } finally {
      setIsSyncing(false);
    }
  };

  const saveMutation = useMutation({
    mutationFn: async (input: LLMProviderInput) => {
      if (!orgUuid) {
        throw new Error(msg('llmModels.noOrganization', 'No organization is available.'));
      }
      return scope === 'organization'
        ? upsertOrganizationLLMProvider(orgUuid, input, csrfToken)
        : persistLLMProvider(orgUuid, workspaceId, editing, input, csrfToken);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (provider: LLMProvider) =>
      scope === 'organization'
        ? deleteOrganizationLLMProvider(orgUuid ?? '', csrfToken)
        : deleteLLMProvider(orgUuid ?? '', workspaceId, provider.id, csrfToken),
    onSuccess: async () => {
      setDeleting(null);
      await invalidateCatalog();
    },
  });

  const updateModelsMutation = useMutation({
    mutationFn: async ({ provider, modelIds }: { provider: LLMProvider; modelIds: string[] }) => {
      if (!orgUuid) {
        throw new Error(msg('llmModels.noOrganization', 'No organization is available.'));
      }
      const input = providerInput(provider, modelIds);
      return scope === 'organization'
        ? upsertOrganizationLLMProvider(orgUuid, input, csrfToken)
        : updateLLMProvider(orgUuid, workspaceId, provider.id, input, csrfToken);
    },
    onSuccess: async () => {
      setQuery('');
      await invalidateCatalog();
    },
    onError: (error) => {
      toast.error(readableError(error, msg), { id: 'llm-models-update' });
    },
  });

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
    setFormError('');
    saveMutation.reset();
    if (refreshAfterSaveRef.current) {
      refreshAfterSaveRef.current = false;
      void invalidateCatalog();
    }
  };

  const openCreate = () => {
    setEditing(null);
    setFormError('');
    setFormOpen(true);
  };

  const submitForm = async (input: LLMProviderInput) => {
    try {
      await saveMutation.mutateAsync(input);
      refreshAfterSaveRef.current = true;
      return true;
    } catch (error) {
      setFormError(readableError(error, msg));
      return false;
    }
  };

  const addModel = (provider: LLMProvider, modelId: string) => {
    updateModelsMutation.mutate({ provider, modelIds: [...provider.models.map((model) => model.id), modelId] });
  };

  const refreshCatalog = async () => {
    const targets = (
      activeProviderId === ALL_PROVIDERS_ID
        ? providers
        : providers.filter((provider) => provider.id === activeProviderId)
    ).filter((provider) => scope === 'organization' || provider.scope !== 'organization');
    try {
      await syncLiveModels(targets);
    } catch (error) {
      toast.error(readableError(error, msg), { id: 'llm-models-sync' });
    }
    await invalidateCatalog();
  };

  return (
    <section className="w-full max-w-none" data-testid="llm-models-page">
      <CatalogHeader
        scope={scope}
        modelCount={catalog.length}
        providerCount={providers.length}
        canManage={canManage}
        accessDenied={catalogAccessDenied}
        onCreate={openCreate}
      />

      <CatalogBody
        canManage={canManage}
        organizationEditable={scope === 'organization'}
        providers={providers}
        catalog={catalog}
        selectedProviderId={activeProviderId}
        query={query}
        isLoading={providersQuery.isLoading || organizationQuery.isLoading}
        providersError={providersError}
        organizationError={scope === 'workspace' ? organizationQuery.error : null}
        isRefreshing={providersQuery.isFetching || organizationQuery.isFetching || isSyncing}
        isAddingModel={updateModelsMutation.isPending}
        onRetry={() => void invalidateCatalog()}
        onOpenCreate={openCreate}
        onQueryChange={setQuery}
        onRefresh={() => void (canManage ? refreshCatalog() : invalidateCatalog())}
        onSelectProvider={setSelectedProviderId}
        onEditProvider={(provider) => {
          setEditing(provider);
          setFormError('');
          setFormOpen(true);
        }}
        onDeleteProvider={setDeleting}
        onAddModel={addModel}
      />

      {formOpen ? (
        <ProviderFormDialog
          presetCatalog={presetsQuery.data}
          presetError={presetsQuery.error ? readableError(presetsQuery.error, msg) : undefined}
          onReloadPresets={() => void presetsQuery.refetch()}
          provider={editing}
          organization={scope === 'organization'}
          error={formError}
          isPending={saveMutation.isPending}
          onClose={closeForm}
          onSubmit={submitForm}
          onDiscoverModels={discoverModels}
        />
      ) : null}
      <ProviderDeleteDialog
        provider={deleting}
        error={deleteMutation.isError ? readableError(deleteMutation.error, msg) : ''}
        isPending={deleteMutation.isPending}
        onClose={() => {
          setDeleting(null);
          deleteMutation.reset();
        }}
        onConfirm={(provider) => deleteMutation.mutate(provider)}
      />
    </section>
  );
}

function CatalogHeader({
  scope,
  modelCount,
  providerCount,
  canManage,
  accessDenied,
  onCreate,
}: {
  scope: ProviderScope;
  modelCount: number;
  providerCount: number;
  canManage: boolean;
  accessDenied: boolean;
  onCreate: () => void;
}) {
  const { msg } = useI18n();
  return (
    <ResourcePageHeader
      title={
        scope === 'organization' ? msg('nav.llmSettings', 'LLM configuration') : msg('llmModels.title', 'LLM models')
      }
      titleAdornment={modelCount > 0 && !accessDenied ? <Badge variant="secondary">{modelCount}</Badge> : undefined}
      description={
        scope === 'organization'
          ? msg(
              'llmModels.organizationDescription',
              'The organization default is shared by all workspaces. Model IDs without a provider prefix prefer workspace providers.',
            )
          : undefined
      }
      actions={
        canManage && providerCount > 0 && scope === 'workspace' && !accessDenied ? (
          <Button type="button" size="lg" onClick={onCreate}>
            <Plus className="size-4" aria-hidden />
            {msg('llmModels.addProvider', 'Add provider')}
          </Button>
        ) : undefined
      }
    />
  );
}

function CatalogBody({
  canManage,
  organizationEditable,
  organizationError,
  providers,
  catalog,
  selectedProviderId,
  query,
  isLoading,
  providersError,
  isRefreshing,
  isAddingModel,
  onRetry,
  onOpenCreate,
  onQueryChange,
  onRefresh,
  onSelectProvider,
  onEditProvider,
  onDeleteProvider,
  onAddModel,
}: {
  canManage: boolean;
  organizationEditable: boolean;
  organizationError: unknown;
  providers: LLMProvider[];
  catalog: CatalogModel[];
  selectedProviderId: string;
  query: string;
  isLoading: boolean;
  providersError: unknown;
  isRefreshing: boolean;
  isAddingModel: boolean;
  onRetry: () => void;
  onOpenCreate: () => void;
  onQueryChange: (query: string) => void;
  onRefresh: () => void;
  onSelectProvider: (providerId: string) => void;
  onEditProvider: (provider: LLMProvider) => void;
  onDeleteProvider: (provider: LLMProvider) => void;
  onAddModel: (provider: LLMProvider, modelId: string) => void;
}) {
  const { msg } = useI18n();
  if (isCatalogAccessDenied(providersError)) {
    return (
      <AccessDenied
        title={msg('llmModels.accessDeniedTitle', 'Access denied')}
        description={msg(
          'llmModels.accessDeniedDescription',
          'You do not have permission to view models in this workspace. Workspace members can view the catalog read-only, and only organization administrators can manage providers.',
        )}
      />
    );
  }
  if (providersError) {
    return (
      <Alert variant="destructive" className="[overflow-wrap:anywhere]">
        <AlertCircle aria-hidden />
        <AlertDescription>
          <p>{readableError(providersError, msg)}</p>
          <Button type="button" variant="outline" size="sm" className="mt-2" disabled={isRefreshing} onClick={onRetry}>
            {msg('common.retry', 'Retry')}
          </Button>
        </AlertDescription>
      </Alert>
    );
  }
  if (isLoading) {
    return <CatalogSkeleton />;
  }

  return (
    <>
      {organizationError ? (
        <Alert variant="destructive" className="mb-4">
          <AlertDescription>
            {msg(
              'llmModels.organizationUnavailable',
              'The organization default could not be loaded. Workspace providers are still available.',
            )}
          </AlertDescription>
        </Alert>
      ) : null}
      {providers.length === 0 ? (
        <Empty className="min-h-[320px] border-0">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Bot aria-hidden />
            </EmptyMedia>
            <EmptyTitle>{msg('llmModels.catalogEmptyTitle', 'No providers yet')}</EmptyTitle>
          </EmptyHeader>
          {canManage ? (
            <EmptyContent>
              <Button type="button" onClick={onOpenCreate}>
                {msg('llmModels.addProvider', 'Add provider')}
              </Button>
            </EmptyContent>
          ) : null}
        </Empty>
      ) : (
        <ModelCatalog
          organizationEditable={organizationEditable}
          canManage={canManage}
          providers={providers}
          models={catalog}
          selectedProviderId={selectedProviderId}
          query={query}
          isRefreshing={isRefreshing}
          isAddingModel={isAddingModel}
          onQueryChange={onQueryChange}
          onRefresh={onRefresh}
          onSelectProvider={onSelectProvider}
          onEditProvider={onEditProvider}
          onDeleteProvider={onDeleteProvider}
          onAddModel={onAddModel}
        />
      )}
    </>
  );
}

function isCatalogAccessDenied(error: unknown) {
  return Boolean(error && typeof error === 'object' && (error as Partial<ApiError>).status === 403);
}

function CatalogSkeleton() {
  return (
    <div className="grid gap-4 lg:grid-cols-[240px_minmax(0,1fr)]">
      <div className="space-y-2">
        <Skeleton className="h-14 w-full rounded-xl" />
        <Skeleton className="h-14 w-full rounded-xl" />
      </div>
      <div className="overflow-hidden rounded-xl ring-[0.5px] ring-border">
        <div className="border-b-[0.5px] border-border p-3">
          <Skeleton className="h-8 w-full" />
        </div>
        <Skeleton className="h-12 w-full rounded-none" />
        <Skeleton className="h-12 w-full rounded-none" />
        <Skeleton className="h-12 w-full rounded-none" />
      </div>
    </div>
  );
}
