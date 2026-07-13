import { useI18n } from "../../../shared/i18n";
import { Button } from "../../../shared/ui/button";
import { Card, CardContent } from "../../../shared/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "../../../shared/ui/collapsible";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../../shared/ui/dialog";
import { ChevronDown, X } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { compactAgentId } from "../agents/AgentsResourcePage";
import { listAgents, listManagedEntities, localTimezone } from "../api";
import {
  DeploymentAddSelectField,
  DeploymentSelectField,
  DeploymentTextArea,
  DeploymentTextField,
  LockedAgentReferenceField,
  ManagedSelectField,
  ManagedTextArea,
  ManagedTextField,
  VaultMultiSelect,
} from "../components/common";
import { entityDialogSubtitle, submitLabel } from "../labels";
import {
  type AgentApiResponse,
  type AgentPageResponse,
  type CredentialFormValues,
  type EntityOption,
  type EnvironmentApiResponse,
  type ManagedEntityApiResponse,
  type ManagedEntityFormValues,
  type ManagedEntitySection,
  type MemoryApiResponse,
  type MemoryFormValues,
  type MemoryStoreApiResponse,
  type PageResponse,
  type VaultApiResponse,
  type VaultCredentialApiResponse,
} from "../types";
import { errorMessage } from "../utils";
import { credentialFormValues, initialFormValues } from "./model";

export function CredentialDialog({
  credential,
  onClose,
  onSubmit,
}: {
  credential?: VaultCredentialApiResponse;
  onClose: () => void;
  onSubmit: (values: CredentialFormValues) => Promise<void>;
}) {
  const [values, setValues] = useState<CredentialFormValues>(() => credentialFormValues(credential));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const canSubmit =
    values.displayName.trim() &&
    (values.authType === "static_bearer"
      ? values.mcpServerUrl.trim() && values.token.trim()
      : values.secretName.trim() && values.secretValue.trim());

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(values);
    } catch (submitError) {
      setError(errorMessage(submitError));
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[520px]">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{credential ? "Edit credential" : "Add credential"}</DialogTitle>
            <DialogDescription>Store a credential for MCP servers or environment variables.</DialogDescription>
          </DialogHeader>
          <div className="mt-5 space-y-4">
            <ManagedTextField
              label="Display name"
              value={values.displayName}
              onChange={(displayName) => setValues((current) => ({ ...current, displayName }))}
              autoFocus
            />
            <ManagedSelectField
              label="Auth type"
              value={values.authType}
              placeholder="Auth type"
              options={[
                { id: "static_bearer", label: "Static bearer" },
                { id: "environment_variable", label: "Environment variable" },
              ]}
              onChange={(authType) =>
                setValues((current) => ({
                  ...current,
                  authType: authType === "environment_variable" ? "environment_variable" : "static_bearer",
                }))
              }
            />
            {values.authType === "static_bearer" ? (
              <>
                <ManagedTextField
                  label="MCP server URL"
                  value={values.mcpServerUrl}
                  placeholder="https://example.com/mcp"
                  onChange={(mcpServerUrl) => setValues((current) => ({ ...current, mcpServerUrl }))}
                />
                <ManagedTextField
                  label="Token"
                  value={values.token}
                  placeholder="Token"
                  onChange={(token) => setValues((current) => ({ ...current, token }))}
                />
              </>
            ) : (
              <>
                <ManagedTextField
                  label="Secret name"
                  value={values.secretName}
                  placeholder="EXAMPLE_TOKEN"
                  onChange={(secretName) => setValues((current) => ({ ...current, secretName }))}
                />
                <ManagedTextField
                  label="Secret value"
                  value={values.secretValue}
                  placeholder="Secret value"
                  onChange={(secretValue) => setValues((current) => ({ ...current, secretValue }))}
                />
              </>
            )}
          </div>
          {error ? <p className="mt-4 text-sm text-destructive">{error}</p> : null}
          <DialogFooter className="mt-5">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canSubmit || submitting}>
              {submitting ? "Saving..." : credential ? "Save changes" : "Add credential"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function MemoryDialog({
  memory,
  onClose,
  onSubmit,
}: {
  memory?: MemoryApiResponse;
  onClose: () => void;
  onSubmit: (values: MemoryFormValues) => Promise<void>;
}) {
  const [values, setValues] = useState<MemoryFormValues>(() => ({
    path: memory?.path || "",
    content: memory?.content || "",
  }));
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const canSubmit = values.path.trim() && values.content.length > 0;
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      await onSubmit(values);
    } catch (submitError) {
      setError(errorMessage(submitError));
      setSubmitting(false);
    }
  };
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-[560px]">
        <form onSubmit={submit}>
          <DialogHeader>
            <DialogTitle>{memory ? "Edit memory" : "Add memory"}</DialogTitle>
            <DialogDescription>Persist a path and content value in this memory store.</DialogDescription>
          </DialogHeader>
          <div className="mt-5 space-y-4">
            <ManagedTextField
              label="Path"
              value={values.path}
              placeholder="/notes/example.txt"
              onChange={(path) => setValues((current) => ({ ...current, path }))}
              autoFocus
            />
            <ManagedTextArea
              label="Content"
              value={values.content}
              placeholder="Memory content"
              onChange={(content) => setValues((current) => ({ ...current, content }))}
            />
          </div>
          {error ? <p className="mt-4 text-sm text-destructive">{error}</p> : null}
          <DialogFooter className="mt-5">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canSubmit || submitting}>
              {submitting ? "Saving..." : memory ? "Save changes" : "Add memory"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

export function ManagedEntityDialog({
  section,
  title,
  entity,
  lockedAgent,
  workspaceId,
  onClose,
  onSubmit,
}: {
  section: ManagedEntitySection;
  title: string;
  entity?: ManagedEntityApiResponse;
  lockedAgent?: AgentApiResponse;
  workspaceId: string;
  onClose: () => void;
  onSubmit: (values: ManagedEntityFormValues) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [values, setValues] = useState<ManagedEntityFormValues>(() => ({
    ...initialFormValues(section, entity),
    ...(lockedAgent ? { agentId: lockedAgent.id } : {}),
  }));
  const [agents, setAgents] = useState<EntityOption[]>([]);
  const [environments, setEnvironments] = useState<EntityOption[]>([]);
  const [vaults, setVaults] = useState<EntityOption[]>([]);
  const [memoryStores, setMemoryStores] = useState<EntityOption[]>([]);
  const [loadingOptions, setLoadingOptions] = useState(section === "sessions" || section === "deployments");
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [resourceDisclosureOpen, setResourceDisclosureOpen] = useState(false);
  const needsReferences = section === "sessions" || section === "deployments";

  useEffect(() => {
    if (!needsReferences) {
      return;
    }
    let active = true;

    void (async () => {
      await Promise.resolve();
      if (!active) {
        return;
      }

      setLoadingOptions(true);
      try {
        const [agentPage, environmentPage, vaultPage, memoryStorePage] = await Promise.all([
          lockedAgent ? Promise.resolve({ data: [], next_page: null } as AgentPageResponse) : listAgents(workspaceId),
          listManagedEntities("environments", workspaceId),
          listManagedEntities("credential-vaults", workspaceId),
          section === "deployments"
            ? listManagedEntities("memory-stores", workspaceId)
            : Promise.resolve({ data: [], next_page: null } as PageResponse<ManagedEntityApiResponse>),
        ]);
        if (!active) {
          return;
        }
        const agentOptions = lockedAgent
          ? [
              {
                id: lockedAgent.id,
                label: lockedAgent.name || lockedAgent.id,
                secondary: `v${lockedAgent.version} · ${compactAgentId(lockedAgent.id)}`,
              },
            ]
          : (agentPage.data ?? []).map((agent) => ({
              id: agent.id,
              label: agent.name || agent.id,
              secondary: compactAgentId(agent.id),
            }));
        const environmentOptions = (environmentPage.data as EnvironmentApiResponse[]).map((environment) => ({
          id: environment.id,
          label: environment.name || environment.id,
          secondary: environment.id,
        }));
        const vaultOptions = (vaultPage.data as VaultApiResponse[]).map((vault) => ({
          id: vault.id,
          label: vault.display_name || vault.id,
          secondary: vault.id,
        }));
        const memoryStoreOptions = (memoryStorePage.data as MemoryStoreApiResponse[]).map((memoryStore) => ({
          id: memoryStore.id,
          label: memoryStore.name || memoryStore.id,
          secondary: memoryStore.id,
        }));
        setAgents(agentOptions);
        setEnvironments(environmentOptions);
        setVaults(vaultOptions);
        setMemoryStores(memoryStoreOptions);
        setValues((current) => ({
          ...current,
          agentId: lockedAgent?.id || current.agentId || (section === "sessions" ? agentOptions[0]?.id || "" : ""),
          environmentId: current.environmentId || (section === "sessions" ? environmentOptions[0]?.id || "" : ""),
        }));
        setLoadingOptions(false);
      } catch (error) {
        if (active) {
          setSubmitError(errorMessage(error));
          setLoadingOptions(false);
        }
      }
    })();

    return () => {
      active = false;
    };
  }, [lockedAgent, needsReferences, section, workspaceId]);

  const canSubmit =
    section === "deployments"
      ? values.name.trim().length > 0 &&
        values.agentId.trim().length > 0 &&
        values.environmentId.trim().length > 0 &&
        values.initialMessage.trim().length > 0 &&
        (values.triggerType === "manual" ||
          (values.triggerType === "schedule" &&
            values.cronExpression.trim().length > 0 &&
            values.timezone.trim().length > 0)) &&
        !submitting &&
        !loadingOptions
      : section === "sessions"
        ? (!needsReferences || (values.agentId.trim().length > 0 && values.environmentId.trim().length > 0)) &&
          !submitting &&
          !loadingOptions
        : values.name.trim().length > 0 &&
          (!needsReferences || (values.agentId.trim().length > 0 && values.environmentId.trim().length > 0)) &&
          !submitting &&
          !loadingOptions;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setSubmitting(true);
    setSubmitError(null);
    try {
      await onSubmit(values);
    } catch (error) {
      setSubmitError(errorMessage(error));
      setSubmitting(false);
    }
  };
  const dialogSubtitleText = entityDialogSubtitle(section, msg);

  if (section === "deployments") {
    return (
      <Dialog open onOpenChange={(open) => !open && onClose()}>
        <DialogContent
          className="flex max-h-[min(760px,calc(100dvh-2rem))] flex-col sm:max-w-[560px]"
          showCloseButton={false}
        >
          <form className="relative flex min-h-0 flex-col" onSubmit={handleSubmit}>
            <DialogClose
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={msg("common.close", "Close")}
                  className="absolute right-0 top-0 text-foreground hover:bg-accent"
                />
              }
            >
              <X className="size-[22px]" aria-hidden />
            </DialogClose>

            <div className="pr-8">
              <DialogTitle className="text-[22px] font-semibold leading-[26px] text-foreground">{title}</DialogTitle>
              <DialogDescription className="mt-1 text-sm leading-5 text-muted-foreground">
                {msg(
                  "managedAgents.deployments.dialogSubtitle",
                  "Deploy an agent with a trigger, environment, and credentials.",
                )}
              </DialogDescription>
            </div>

            <div className="subtle-scrollbar mt-5 min-h-0 flex-1 space-y-[18px] overflow-y-auto pr-1">
              <DeploymentTextField
                label={msg("common.name", "Name")}
                value={values.name}
                placeholder={msg("managedAgents.deployments.namePlaceholder", "Nightly inbox triage")}
                onChange={(name) => setValues((current) => ({ ...current, name }))}
                autoFocus
              />
              {lockedAgent ? (
                <LockedAgentReferenceField agent={lockedAgent} variant="deployment" />
              ) : (
                <DeploymentSelectField
                  label={msg("managedAgents.common.agent", "Agent")}
                  value={values.agentId}
                  placeholder={
                    loadingOptions
                      ? msg("managedAgents.agents.loading", "Loading agents...")
                      : msg("managedAgents.deployments.selectAgent", "Select an agent")
                  }
                  options={agents}
                  manageHref={`/workspaces/${workspaceId}/agents`}
                  manageLabel={msg("managedAgents.agents.manage", "Manage agents")}
                  onChange={(agentId) => setValues((current) => ({ ...current, agentId }))}
                />
              )}
              <DeploymentTextArea
                label={msg("managedAgents.deployments.initialMessage", "Initial message")}
                value={values.initialMessage}
                placeholder={msg(
                  "managedAgents.deployments.initialMessagePlaceholder",
                  "Summarize today's support tickets and post to #digest",
                )}
                helpText={msg(
                  "managedAgents.deployments.initialMessageHelp",
                  "Sent to the agent at the start of every run.",
                )}
                onChange={(initialMessage) => setValues((current) => ({ ...current, initialMessage }))}
              />
              <DeploymentSelectField
                label={msg("managedAgents.environments.kindTitle", "Environment")}
                value={values.environmentId}
                placeholder={
                  loadingOptions
                    ? msg("managedAgents.environments.loading", "Loading environments...")
                    : msg("managedAgents.quickstart.selectEnvironment", "Select an environment")
                }
                options={environments}
                manageHref={`/workspaces/${workspaceId}/environments`}
                manageLabel={msg("managedAgents.environments.manage", "Manage environments")}
                onChange={(environmentId) => setValues((current) => ({ ...current, environmentId }))}
              />
              <DeploymentAddSelectField
                label={msg("managedAgents.credentialVaults.title", "Credential vaults")}
                optional
                valueLabel={msg("managedAgents.credentialVaults.kind", "vault")}
                selectedIds={values.vaultIds}
                options={vaults}
                manageHref={`/workspaces/${workspaceId}/vaults`}
                manageLabel={msg("managedAgents.credentialVaults.manage", "Manage credential vaults")}
                onChange={(vaultIds) => setValues((current) => ({ ...current, vaultIds }))}
              />
              <DeploymentAddSelectField
                label={msg("managedAgents.memoryStores.title", "Memory stores")}
                optional
                valueLabel={msg("managedAgents.memoryStores.kind", "memory store")}
                selectedIds={values.memoryStoreIds}
                options={memoryStores}
                manageHref={`/workspaces/${workspaceId}/memory-stores`}
                manageLabel={msg("managedAgents.memoryStores.manage", "Manage memory stores")}
                onChange={(memoryStoreIds) => setValues((current) => ({ ...current, memoryStoreIds }))}
              />
              <DeploymentSelectField
                label={msg("managedAgents.common.trigger", "Trigger")}
                value={values.triggerType}
                placeholder={msg("managedAgents.deployments.selectTrigger", "Select a trigger")}
                options={[
                  { id: "manual", label: msg("managedAgents.deployments.trigger.manual", "Manual") },
                  { id: "schedule", label: msg("managedAgents.deployments.trigger.scheduled", "Scheduled") },
                ]}
                onChange={(triggerType) =>
                  setValues((current) => ({
                    ...current,
                    triggerType: triggerType === "schedule" ? "schedule" : triggerType === "manual" ? "manual" : "",
                  }))
                }
              />
              {values.triggerType === "schedule" ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  <DeploymentTextField
                    label={msg("managedAgents.deployments.cronExpression", "Cron expression")}
                    value={values.cronExpression}
                    placeholder="0 9 * * 1"
                    onChange={(cronExpression) => setValues((current) => ({ ...current, cronExpression }))}
                  />
                  <DeploymentTextField
                    label={msg("managedAgents.deployments.timezone", "Timezone")}
                    value={values.timezone}
                    placeholder={localTimezone()}
                    onChange={(timezone) => setValues((current) => ({ ...current, timezone }))}
                  />
                </div>
              ) : null}
            </div>

            {submitError ? <p className="mt-4 text-sm text-destructive">{submitError}</p> : null}

            <div className="mt-5 flex justify-end">
              <Button type="submit" disabled={!canSubmit}>
                {submitting ? msg("common.saving", "Saving...") : submitLabel(section, Boolean(entity), msg)}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        className="flex max-h-[min(760px,calc(100dvh-2rem))] flex-col sm:max-w-[560px]"
        showCloseButton={false}
      >
        <form className="relative flex min-h-0 flex-col" onSubmit={handleSubmit}>
          <DialogClose
            render={
              <Button
                type="button"
                variant="ghost"
                size="icon"
                aria-label={msg("common.close", "Close")}
                className="absolute right-0 top-0 text-foreground hover:bg-accent"
              />
            }
          >
            <X className="size-[22px]" aria-hidden />
          </DialogClose>

          <div className="pr-8">
            <DialogTitle className="text-[22px] font-semibold leading-[26px] text-foreground">{title}</DialogTitle>
            {dialogSubtitleText ? (
              <DialogDescription className="mt-1 text-sm leading-5 text-muted-foreground">
                {dialogSubtitleText}
              </DialogDescription>
            ) : null}
          </div>

          <div className="subtle-scrollbar mt-5 min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
            <ManagedTextField
              label={
                section === "sessions" ? msg("managedAgents.sessions.fieldTitle", "Title") : msg("common.name", "Name")
              }
              value={values.name}
              placeholder={
                section === "sessions"
                  ? msg("managedAgents.sessions.titlePlaceholder", "Optional - name this run")
                  : msg("managedAgents.common.namePlaceholder", "Enter a name")
              }
              onChange={(name) => setValues((current) => ({ ...current, name }))}
              autoFocus
            />

            {section === "environments" ? (
              <>
                <ManagedTextField
                  label={msg("managedAgents.environments.hostingType", "Hosting type")}
                  value={msg("managedAgents.environments.cloud", "Cloud")}
                  disabled
                  onChange={() => undefined}
                />
                <ManagedTextArea
                  label={msg("common.description", "Description")}
                  value={values.description}
                  placeholder={msg("managedAgents.common.descriptionPlaceholder", "Add a description")}
                  onChange={(description) => setValues((current) => ({ ...current, description }))}
                />
              </>
            ) : null}

            {section === "memory-stores" ? (
              <ManagedTextArea
                label={msg("common.description", "Description")}
                value={values.description}
                placeholder={msg("managedAgents.common.descriptionPlaceholder", "Add a description")}
                onChange={(description) => setValues((current) => ({ ...current, description }))}
              />
            ) : null}

            {needsReferences ? (
              <>
                {lockedAgent ? (
                  <LockedAgentReferenceField agent={lockedAgent} variant="managed" />
                ) : (
                  <ManagedSelectField
                    label={msg("managedAgents.common.agent", "Agent")}
                    value={values.agentId}
                    placeholder={
                      loadingOptions
                        ? msg("managedAgents.agents.loading", "Loading agents...")
                        : msg("managedAgents.deployments.selectAgent", "Select an agent")
                    }
                    options={agents}
                    onChange={(agentId) => setValues((current) => ({ ...current, agentId }))}
                  />
                )}
                <ManagedSelectField
                  label={msg("managedAgents.environments.kindTitle", "Environment")}
                  value={values.environmentId}
                  placeholder={
                    loadingOptions
                      ? msg("managedAgents.environments.loading", "Loading environments...")
                      : msg("managedAgents.quickstart.selectEnvironment", "Select an environment")
                  }
                  options={environments}
                  onChange={(environmentId) => setValues((current) => ({ ...current, environmentId }))}
                />
                <VaultMultiSelect
                  vaults={vaults}
                  selectedIds={values.vaultIds}
                  onChange={(vaultIds) => setValues((current) => ({ ...current, vaultIds }))}
                />
                <Collapsible open={resourceDisclosureOpen} onOpenChange={setResourceDisclosureOpen}>
                  <Card size="sm" className="gap-0 py-0">
                    <CollapsibleTrigger
                      type="button"
                      className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm font-semibold text-foreground transition-colors hover:bg-accent/40 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                    >
                      <ChevronDown
                        className={`size-4 shrink-0 transition-transform duration-200 motion-reduce:transition-none ${resourceDisclosureOpen ? "" : "-rotate-90"}`}
                        aria-hidden
                      />
                      <span>{msg("managedAgents.common.resource", "Resource")}</span>
                    </CollapsibleTrigger>
                    <CollapsibleContent className="border-t border-border">
                      <CardContent className="px-3 pb-3 pt-2">
                        <p className="text-sm leading-5 text-muted-foreground">
                          {msg(
                            "managedAgents.common.noResourceAttachments",
                            "No resource attachments are configured. Add files, repositories, or memory stores after creation.",
                          )}
                        </p>
                      </CardContent>
                    </CollapsibleContent>
                  </Card>
                </Collapsible>
              </>
            ) : null}

            {section === "credential-vaults" ? (
              <p className="text-sm leading-5 text-muted-foreground">
                {msg(
                  "managedAgents.credentialVaults.createHint",
                  "Continue after creating the vault to add credentials for tools and MCP servers.",
                )}
              </p>
            ) : null}
          </div>

          {submitError ? <p className="mt-4 text-sm text-destructive">{submitError}</p> : null}

          <div className="mt-5 flex justify-end gap-2">
            <DialogClose render={<Button type="button" variant="outline" />}>
              {msg("common.cancel", "Cancel")}
            </DialogClose>
            <Button type="submit" disabled={!canSubmit}>
              {submitting ? msg("common.saving", "Saving...") : submitLabel(section, Boolean(entity), msg)}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
