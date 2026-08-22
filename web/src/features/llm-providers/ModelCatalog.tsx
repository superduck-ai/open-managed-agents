import { Check, Copy, KeyRound, Pencil, Plus, RefreshCw, Search, Trash2 } from 'lucide-react';
import { useMemo, useState } from 'react';
import { copyText } from '@/shared/lib/clipboard';
import { cn } from '@/shared/lib/utils';
import { useI18n } from '../../shared/i18n';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '../../shared/ui/accordion';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { Card } from '../../shared/ui/card';
import { InputGroup, InputGroupAddon, InputGroupInput } from '../../shared/ui/input-group';
import type { LLMProvider } from './api';
import {
  ALL_PROVIDERS_ID,
  addableModelId,
  filterCatalogModels,
  groupCatalogModels,
  modelsForProvider,
  providerHost,
  type CatalogGroup,
  type CatalogModel,
} from './catalog';

type ModelCatalogProps = {
  providers: LLMProvider[];
  models: CatalogModel[];
  selectedProviderId: string;
  query: string;
  isRefreshing: boolean;
  isAddingModel: boolean;
  onQueryChange: (query: string) => void;
  onRefresh: () => void;
  onSelectProvider: (providerId: string) => void;
  onEditProvider: (provider: LLMProvider) => void;
  onDeleteProvider: (provider: LLMProvider) => void;
  onAddModel: (provider: LLMProvider, modelId: string) => void;
};

export function ModelCatalog({
  providers,
  models,
  selectedProviderId,
  query,
  isRefreshing,
  isAddingModel,
  onQueryChange,
  onRefresh,
  onSelectProvider,
  onEditProvider,
  onDeleteProvider,
  onAddModel,
}: ModelCatalogProps) {
  const { msg } = useI18n();
  const selected = providers.find((provider) => provider.id === selectedProviderId);
  const visibleModels = modelsForProvider(filterCatalogModels(models, query), selectedProviderId);
  const candidateId = addableModelId(query, models);

  return (
    <div className="grid items-start gap-4 lg:grid-cols-[240px_minmax(0,1fr)]">
      <ProviderRail
        providers={providers}
        models={models}
        selectedProviderId={selectedProviderId}
        onSelectProvider={onSelectProvider}
      />
      <Card className="min-w-0 gap-0 py-0">
        <div className="flex items-center gap-2 border-b border-border bg-muted/20 p-3">
          <InputGroup className="h-9 flex-1 bg-background shadow-none">
            <InputGroupAddon>
              <Search aria-hidden />
            </InputGroupAddon>
            <InputGroupInput
              value={query}
              onChange={(event) => onQueryChange(event.target.value)}
              placeholder={msg('llmModels.searchPlaceholder', 'Add or search model')}
              aria-label={msg('llmModels.searchPlaceholder', 'Add or search model')}
            />
          </InputGroup>
          <Button
            type="button"
            size="icon-lg"
            variant="outline"
            aria-label={msg('llmModels.refreshModels', 'Refresh model list')}
            disabled={isRefreshing}
            onClick={onRefresh}
          >
            <RefreshCw className={cn('size-4', isRefreshing && 'animate-spin')} aria-hidden />
          </Button>
        </div>

        {selected ? (
          <ProviderSection
            provider={selected}
            models={visibleModels}
            emptyMessage={
              query.trim()
                ? msg('llmModels.noSearchResults', 'No models match this search.')
                : msg('llmModels.emptyModels', 'No models configured for this provider.')
            }
            onEdit={() => onEditProvider(selected)}
            onDelete={() => onDeleteProvider(selected)}
          />
        ) : (
          <ProviderModelGroups
            groups={groupCatalogModels(visibleModels, providers, !query.trim())}
            providers={providers}
            query={query}
          />
        )}

        <AddModelHint
          candidateId={candidateId}
          query={query}
          provider={selected}
          isAdding={isAddingModel}
          onAdd={() => {
            if (selected && candidateId) onAddModel(selected, candidateId);
          }}
        />
      </Card>
    </div>
  );
}

function ProviderRail({
  providers,
  models,
  selectedProviderId,
  onSelectProvider,
}: {
  providers: LLMProvider[];
  models: CatalogModel[];
  selectedProviderId: string;
  onSelectProvider: (providerId: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <nav
      className="flex gap-1 overflow-x-auto rounded-xl bg-muted/30 p-1 lg:flex-col lg:overflow-visible"
      data-testid="llm-provider-rail"
      aria-label={msg('llmModels.allProviders', 'All providers')}
    >
      <ProviderRailButton
        label={msg('llmModels.allProviders', 'All providers')}
        count={models.length}
        selected={selectedProviderId === ALL_PROVIDERS_ID}
        onClick={() => onSelectProvider(ALL_PROVIDERS_ID)}
      />
      {providers.map((provider) => (
        <ProviderRailButton
          key={provider.id}
          label={provider.name}
          detail={providerHost(provider.base_url)}
          count={provider.model_ids.length}
          selected={selectedProviderId === provider.id}
          onClick={() => onSelectProvider(provider.id)}
        />
      ))}
    </nav>
  );
}

function ProviderRailButton({
  label,
  detail,
  count,
  selected,
  onClick,
}: {
  label: string;
  detail?: string;
  count: number;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant={selected ? 'secondary' : 'ghost'}
      onClick={onClick}
      aria-label={label}
      aria-pressed={selected}
      className="h-auto min-w-[180px] flex-col items-start gap-0.5 rounded-lg px-3 py-2.5 text-left whitespace-normal lg:w-full lg:min-w-0"
    >
      <span className="flex w-full items-center justify-between gap-2">
        <span className="truncate text-sm font-semibold">{label}</span>
        <Badge
          variant={selected ? 'default' : 'secondary'}
          className="h-6 min-w-6 justify-center px-1.5 font-normal tabular-nums"
        >
          {count}
        </Badge>
      </span>
      {detail ? <span className="w-full truncate text-xs font-normal text-muted-foreground">{detail}</span> : null}
    </Button>
  );
}

function ProviderSection({
  provider,
  models,
  emptyMessage,
  onEdit,
  onDelete,
}: {
  provider: LLMProvider;
  models: CatalogModel[];
  emptyMessage: string;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { msg } = useI18n();
  return (
    <section data-testid={`llm-provider-group-${provider.id}`}>
      <div className="flex items-center gap-3 bg-muted/40 px-4 py-3">
        <ProviderIdentity provider={provider} modelCount={models.length} />
        <div className="flex shrink-0 items-center gap-1">
          <Button type="button" variant="ghost" size="icon-sm" aria-label={msg('common.edit', 'Edit')} onClick={onEdit}>
            <Pencil aria-hidden />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('common.delete', 'Delete')}
            onClick={onDelete}
          >
            <Trash2 aria-hidden />
          </Button>
        </div>
      </div>
      <ModelRows models={models} emptyMessage={emptyMessage} />
    </section>
  );
}

function ProviderIdentity({ provider, modelCount }: { provider: LLMProvider; modelCount: number }) {
  return (
    <div className="flex min-w-0 flex-1 items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="break-words text-sm font-semibold text-foreground">{provider.name}</p>
        <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs font-normal text-muted-foreground">
          <span className="min-w-0 break-all">{provider.base_url}</span>
          <span className="flex shrink-0 items-center gap-1.5">
            <KeyRound className="size-3.5" aria-hidden />
            •••• {provider.api_key_last4}
          </span>
        </div>
      </div>
      <Badge variant="secondary" className="h-6 min-w-6 shrink-0 justify-center px-1.5 font-normal tabular-nums">
        {modelCount}
      </Badge>
    </div>
  );
}

function ProviderModelGroups({
  groups,
  providers,
  query,
}: {
  groups: CatalogGroup[];
  providers: LLMProvider[];
  query: string;
}) {
  const groupValues = useMemo(() => groups.map((group) => group.providerId), [groups]);
  const searching = Boolean(query.trim());
  const [openValues, setOpenValues] = useState<string[] | null>(null);
  if (groups.length === 0) return null;

  return (
    <Accordion
      multiple
      value={searching || openValues === null ? groupValues : openValues}
      onValueChange={(next) => {
        if (!searching) setOpenValues(next);
      }}
    >
      {groups.map((group) => {
        const provider = providers.find((item) => item.id === group.providerId);
        return provider ? (
          <ProviderModelGroup key={group.providerId} group={group} provider={provider} query={query} />
        ) : null;
      })}
    </Accordion>
  );
}

function ProviderModelGroup({ group, provider, query }: { group: CatalogGroup; provider: LLMProvider; query: string }) {
  const { msg } = useI18n();
  return (
    <AccordionItem value={group.providerId} data-testid={`llm-provider-group-${group.providerId}`}>
      <AccordionTrigger
        aria-label={msg('llmModels.providerGroup', '{provider} models', { provider: group.providerName })}
        className="min-h-16 gap-3 rounded-none bg-muted/40 px-4 py-3 hover:bg-muted/60 hover:no-underline"
      >
        <ProviderIdentity provider={provider} modelCount={group.models.length} />
      </AccordionTrigger>
      <AccordionContent className="pb-0">
        <ModelRows
          models={group.models}
          emptyMessage={
            query.trim()
              ? msg('llmModels.noSearchResults', 'No models match this search.')
              : msg('llmModels.emptyModels', 'No models configured for this provider.')
          }
        />
      </AccordionContent>
    </AccordionItem>
  );
}

function ModelRows({ models, emptyMessage }: { models: CatalogModel[]; emptyMessage: string }) {
  if (models.length === 0) {
    return (
      <p className="px-4 py-5 text-sm text-muted-foreground sm:px-6" data-testid="llm-model-empty">
        {emptyMessage}
      </p>
    );
  }
  return (
    <ul className="divide-y divide-border bg-background">
      {models.map((model) => (
        <ModelRow key={`${model.providerId}:${model.id}`} model={model} />
      ))}
    </ul>
  );
}

function ModelRow({ model }: { model: CatalogModel }) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);
  const label = copied ? msg('common.copied', 'Copied') : msg('common.copyId', 'Copy ID');

  const copyId = async () => {
    await copyText(model.id);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <li
      className="flex min-h-11 items-center justify-between gap-3 px-4 py-2 hover:bg-muted/40 sm:px-6"
      data-testid="llm-model-row"
    >
      <p className="min-w-0 break-all font-mono text-sm text-foreground">{model.id}</p>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="shrink-0 text-muted-foreground"
        aria-label={label}
        onClick={() => void copyId()}
      >
        {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
      </Button>
    </li>
  );
}

function AddModelHint({
  candidateId,
  query,
  provider,
  isAdding,
  onAdd,
}: {
  candidateId: string;
  query: string;
  provider?: LLMProvider;
  isAdding: boolean;
  onAdd: () => void;
}) {
  const { msg } = useI18n();
  if (!query.trim() || !candidateId) return null;
  if (!provider) {
    return (
      <p className="px-4 py-6 text-center text-sm text-muted-foreground">
        {msg('llmModels.selectProviderToAdd', 'Select a provider to add {modelId}.', { modelId: candidateId })}
      </p>
    );
  }
  return (
    <div className="border-t border-border p-3">
      <Button type="button" variant="outline" className="w-full justify-start" disabled={isAdding} onClick={onAdd}>
        <Plus aria-hidden />
        {msg('llmModels.addModelToProvider', 'Add {modelId} to {provider}', {
          modelId: candidateId,
          provider: provider.name,
        })}
      </Button>
    </div>
  );
}
