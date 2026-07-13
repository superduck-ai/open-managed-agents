import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useLocation } from "@tanstack/react-router";
import {
  AlertCircle,
  Ban,
  Box,
  Check,
  Copy,
  Info,
  KeyRound,
  Loader2,
  MoreVertical,
  Plus,
  Power,
  Trash2,
} from "lucide-react";
import { useAuth } from "../../shared/auth/context";
import { Alert, AlertDescription } from "../../shared/ui/alert";
import { Badge } from "../../shared/ui/badge";
import { Button } from "../../shared/ui/button";
import { Card, CardContent } from "../../shared/ui/card";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "../../shared/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../shared/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../shared/ui/dropdown-menu";
import { Field, FieldDescription, FieldError, FieldLabel } from "../../shared/ui/field";
import { Input } from "../../shared/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../shared/ui/table";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../shared/ui/tooltip";
import {
  createWorkspaceApiKey,
  defaultWorkspace,
  listWorkspaceApiKeys,
  updateWorkspaceApiKeyStatus,
  type Workspace,
  type WorkspaceApiKey,
} from "../../shared/workspaces/api";
import { useI18n } from "../../shared/i18n";
import { useWorkspace } from "../../shared/workspaces/context";
import { workspaceIdFromPath } from "../../shared/workspaces/presentation";

type WorkspaceApiKeysContentProps = {
  routeWorkspaceId?: string;
};

type KeyAction = "disable" | "enable" | "delete";

type PendingAction = {
  action: KeyAction;
  apiKey: WorkspaceApiKey;
};

type Identity = {
  name: string;
  email: string;
};

export function WorkspaceApiKeysPage() {
  const location = useLocation();
  return <WorkspaceApiKeysContent routeWorkspaceId={workspaceIdFromPath(location.pathname)} />;
}

export function WorkspaceApiKeysContent({ routeWorkspaceId }: WorkspaceApiKeysContentProps) {
  const { account } = useAuth();
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { orgUuid, workspaces, activeWorkspace, activeWorkspaceId, selectWorkspace } = useWorkspace();
  const [createOpen, setCreateOpen] = useState(false);
  const [createdKey, setCreatedKey] = useState<WorkspaceApiKey | null>(null);
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(null);
  const workspace = useMemo(
    () => resolveWorkspace(routeWorkspaceId, workspaces, activeWorkspace),
    [activeWorkspace, routeWorkspaceId, workspaces],
  );
  const queryKey = useMemo(
    () => ["console", "workspace-api-keys", orgUuid, workspace.id] as const,
    [orgUuid, workspace.id],
  );

  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);

  const keysQuery = useQuery({
    queryKey,
    queryFn: () => listWorkspaceApiKeys(orgUuid ?? "", workspace.id),
    enabled: Boolean(orgUuid && workspace.id),
    retry: false,
  });

  const createMutation = useMutation({
    mutationFn: async (name: string) => {
      if (!orgUuid) {
        throw new Error(msg("apiKeys.noOrganizationCreate", "No organization is available for API key creation."));
      }
      return createWorkspaceApiKey(orgUuid, workspace.id, { name });
    },
    onSuccess: async (apiKey) => {
      setCreateOpen(false);
      setCreatedKey(apiKey);
      await queryClient.invalidateQueries({ queryKey });
    },
  });

  const updateMutation = useMutation({
    mutationFn: async ({ apiKey, status }: { apiKey: WorkspaceApiKey; status: "active" | "inactive" | "archived" }) => {
      if (!orgUuid) {
        throw new Error(msg("apiKeys.noOrganizationUpdate", "No organization is available for API key updates."));
      }
      return updateWorkspaceApiKeyStatus(orgUuid, workspace.id, apiKey.id, { status });
    },
    onSuccess: (updatedApiKey, variables) => {
      queryClient.setQueryData<WorkspaceApiKey[]>(queryKey, (current) => {
        const keys = current ?? [];
        if (variables.status === "archived" || isArchivedKey(updatedApiKey)) {
          return keys.filter((apiKey) => apiKey.id !== variables.apiKey.id);
        }
        let replaced = false;
        const next = keys.map((apiKey) => {
          if (apiKey.id !== updatedApiKey.id) {
            return apiKey;
          }
          replaced = true;
          return updatedApiKey;
        });
        return replaced ? next : [updatedApiKey, ...next];
      });
      void queryClient.invalidateQueries({ queryKey });
    },
  });

  const keys = useMemo(() => (keysQuery.data ?? []).filter((apiKey) => !isArchivedKey(apiKey)), [keysQuery.data]);
  const identity = displayIdentity(account);
  const errorMessage = readableError(keysQuery.error);

  const handleCreateKey = async (name: string) => {
    await createMutation.mutateAsync(name);
  };

  const handleConfirmAction = async () => {
    if (!pendingAction) {
      return;
    }
    const status = statusForAction(pendingAction.action);
    await updateMutation.mutateAsync({ apiKey: pendingAction.apiKey, status });
    setPendingAction(null);
  };

  return (
    <TooltipProvider>
      <section className="w-full max-w-none" data-testid="workspace-api-keys-page">
        <div className="mb-5 flex items-start justify-between gap-4">
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-[28px] font-semibold leading-tight tracking-normal text-foreground">
                {msg("apiKeys.title", "API keys")}
              </h1>
              <Badge
                variant="secondary"
                className="h-6 min-w-6 rounded-md px-2 text-muted-foreground"
                aria-label={msg("apiKeys.countAria", "{count, plural, one {# API key} other {# API keys}}", {
                  count: keys.length,
                })}
              >
                {keys.length}
              </Badge>
            </div>
            <p className="mt-2 max-w-[760px] text-sm leading-5 text-muted-foreground">
              {msg(
                "apiKeys.description",
                "API keys are owned by workspaces and remain active even after the creator is removed",
              )}
            </p>
          </div>
          <Button type="button" size="lg" onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" aria-hidden />
            {msg("apiKeys.create", "Create key")}
          </Button>
        </div>

        <Card className="overflow-hidden py-0">
          <CardContent className="p-0">
            <Table className="min-w-[920px] table-fixed text-left">
              <colgroup>
                <col className="w-[30%]" />
                <col className="w-[22%]" />
                <col className="w-[14%]" />
                <col className="w-[14%]" />
                <col className="w-[10%]" />
                <col className="w-[72px]" />
              </colgroup>
              <TableHeader className="text-[13px] text-muted-foreground">
                <TableRow className="border-border hover:bg-transparent">
                  <TableHead className="px-3 py-3 text-muted-foreground">{msg("apiKeys.key", "Key")}</TableHead>
                  <TableHead className="px-3 py-3 text-muted-foreground">
                    {msg("apiKeys.createdBy", "Created by")}
                  </TableHead>
                  <TableHead className="px-3 py-3 text-muted-foreground">
                    {msg("apiKeys.createdAt", "Created at")}
                  </TableHead>
                  <TableHead className="px-3 py-3 text-muted-foreground">
                    {msg("apiKeys.lastUsedAt", "Last used at")}
                  </TableHead>
                  <TableHead className="px-3 py-3 text-muted-foreground">
                    <span className="inline-flex items-center gap-1">
                      {msg("analytics.cost.title", "Cost")}
                      <InfoTooltip
                        label={msg("apiKeys.costTooltip", "API key cost attribution appears after usage is recorded.")}
                      />
                    </span>
                  </TableHead>
                  <TableHead className="px-3 py-3 text-right text-muted-foreground">
                    {msg("common.actions", "Actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keysQuery.isLoading ? (
                  <ApiKeysState
                    text={msg("apiKeys.loading", "Loading {workspaceName} API keys...", {
                      workspaceName: workspace.name,
                    })}
                  />
                ) : errorMessage ? (
                  <ApiKeysState tone="error" text={errorMessage} />
                ) : keys.length > 0 ? (
                  keys.map((apiKey) => (
                    <ApiKeyRow
                      key={apiKey.id}
                      apiKey={apiKey}
                      identity={identity}
                      onAction={(action) => setPendingAction({ action, apiKey })}
                    />
                  ))
                ) : (
                  <ApiKeysState
                    text={msg("apiKeys.empty", "No API keys have been created for {workspaceName}.", {
                      workspaceName: workspace.name,
                    })}
                  />
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        {createOpen ? (
          <CreateApiKeyModal
            workspace={workspace}
            isSubmitting={createMutation.isPending}
            onClose={() => setCreateOpen(false)}
            onCreate={handleCreateKey}
          />
        ) : null}

        {createdKey?.raw_key ? <RawApiKeyModal apiKey={createdKey} onClose={() => setCreatedKey(null)} /> : null}

        {pendingAction ? (
          <ConfirmKeyActionModal
            pendingAction={pendingAction}
            isSubmitting={updateMutation.isPending}
            error={readableError(updateMutation.error)}
            onClose={() => setPendingAction(null)}
            onConfirm={handleConfirmAction}
          />
        ) : null}
      </section>
    </TooltipProvider>
  );
}

function ApiKeyRow({
  apiKey,
  identity,
  onAction,
}: {
  apiKey: WorkspaceApiKey;
  identity: Identity;
  onAction: (action: KeyAction) => void;
}) {
  const { msg } = useI18n();
  const status = normalizeKeyStatus(apiKey);
  const inactive = status === "inactive";
  const creator = displayCreator(apiKey, identity);

  return (
    <TableRow
      className={`h-[58px] border-border text-sm hover:bg-accent ${inactive ? "text-muted-foreground/70" : "text-foreground"}`}
    >
      <TableCell className="min-w-0 px-3 py-2.5 align-middle">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate font-medium">{apiKey.name || msg("apiKeys.untitledKey", "Untitled key")}</span>
          {inactive ? (
            <Badge variant="secondary" className="h-5 shrink-0 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground">
              {msg("apiKeys.inactive", "Inactive")}
            </Badge>
          ) : null}
        </div>
        <div className="mt-1 truncate font-mono text-xs text-muted-foreground">{keyHint(apiKey)}</div>
      </TableCell>
      <TableCell className="min-w-0 px-3 py-2.5 align-middle">
        <div className="truncate">{creator.name}</div>
        <div className="mt-1 truncate text-xs text-muted-foreground">{creator.email}</div>
      </TableCell>
      <TableCell className="px-3 py-2.5 align-middle text-muted-foreground">{formatDate(apiKey.created_at)}</TableCell>
      <TableCell className="px-3 py-2.5 align-middle text-muted-foreground">
        {apiKey.last_used_at ? formatDate(apiKey.last_used_at) : msg("common.never", "Never")}
      </TableCell>
      <TableCell className="px-3 py-2.5 align-middle text-muted-foreground">-</TableCell>
      <TableCell className="px-3 py-2.5 text-right align-middle">
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon"
                className="text-muted-foreground hover:text-foreground"
                aria-label={msg("apiKeys.moreActionsAria", "More actions for {keyName}", {
                  keyName: apiKey.name || msg("apiKeys.thisKey", "this key"),
                })}
              />
            }
          >
            <MoreVertical className="size-4" aria-hidden />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-44">
            <DropdownMenuItem onClick={() => onAction(inactive ? "enable" : "disable")}>
              {inactive ? <Power className="size-4" aria-hidden /> : <Ban className="size-4" aria-hidden />}
              <span>
                {inactive ? msg("apiKeys.enable", "Enable API key") : msg("apiKeys.disable", "Disable API key")}
              </span>
            </DropdownMenuItem>
            <DropdownMenuItem variant="destructive" onClick={() => onAction("delete")}>
              <Trash2 className="size-4" aria-hidden />
              <span>{msg("apiKeys.delete", "Delete API key")}</span>
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
  );
}

function CreateApiKeyModal({
  workspace,
  isSubmitting,
  onClose,
  onCreate,
}: {
  workspace: Workspace;
  isSubmitting: boolean;
  onClose: () => void;
  onCreate: (name: string) => Promise<void>;
}) {
  const { msg } = useI18n();
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const canSubmit = name.trim().length > 0 && !isSubmitting;

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSubmit) {
      return;
    }
    setError("");
    try {
      await onCreate(name.trim());
      setName("");
    } catch (createError) {
      setError(readableError(createError) ?? msg("apiKeys.createFailed", "Failed to create API key."));
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <DialogContent className="sm:max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{msg("apiKeys.createTitle", "Create API key")}</DialogTitle>
        </DialogHeader>

        <form className="space-y-5" onSubmit={handleSubmit}>
          <Field className="gap-2">
            <FieldLabel className="flex items-center gap-1.5">
              {msg("apiKeys.workspace", "Workspace")}
              <InfoTooltip
                label={msg("apiKeys.workspaceTooltip", "This key will be scoped to the selected workspace.")}
              />
            </FieldLabel>
            <FieldDescription className="flex h-5 items-center gap-2 text-sm text-foreground">
              <Box
                className="size-4 shrink-0"
                style={{ color: workspace.display_color || workspace.color }}
                aria-hidden
              />
              <span className="truncate">{workspace.name}</span>
            </FieldDescription>
          </Field>

          <Field data-invalid={Boolean(error)}>
            <FieldLabel htmlFor="api-key-name">{msg("common.name", "Name")}</FieldLabel>
            <Input
              id="api-key-name"
              value={name}
              aria-invalid={Boolean(error) || undefined}
              placeholder={msg("apiKeys.namePlaceholder", "my-secret-key")}
              onChange={(event) => setName(event.target.value)}
            />
            <FieldError className="text-destructive">{error}</FieldError>
          </Field>

          <DialogFooter>
            <Button type="submit" disabled={!canSubmit} className="min-w-[52px]">
              {isSubmitting ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
              {msg("apiKeys.add", "Add")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function RawApiKeyModal({ apiKey, onClose }: { apiKey: WorkspaceApiKey; onClose: () => void }) {
  const { msg } = useI18n();
  const [copied, setCopied] = useState(false);
  const [copyFailed, setCopyFailed] = useState(false);
  const rawKey = apiKey.raw_key ?? "";

  const handleCopy = async () => {
    setCopyFailed(false);
    setCopied(true);
    const didCopy = await copyTextToClipboard(rawKey);
    if (!didCopy) {
      setCopied(false);
      setCopyFailed(true);
      return;
    }
    window.setTimeout(() => setCopied(false), 1400);
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{msg("apiKeys.createdTitle", "API key created")}</DialogTitle>
          <DialogDescription className="leading-6">
            {msg("apiKeys.copyRaw", "Copy this key now. You won't be able to view it again after closing this window.")}
          </DialogDescription>
        </DialogHeader>
        <Card size="sm" className="mt-5 py-0">
          <CardContent className="px-3 py-3">
            <div className="break-all font-mono text-sm leading-6 text-foreground">{rawKey}</div>
          </CardContent>
        </Card>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={handleCopy}>
            {copied ? <Check className="size-4" aria-hidden /> : <Copy className="size-4" aria-hidden />}
            {copied
              ? msg("common.copied", "Copied")
              : copyFailed
                ? msg("common.copyFailed", "Copy failed")
                : msg("common.copy", "Copy")}
          </Button>
          <Button type="button" onClick={onClose}>
            {msg("common.done", "Done")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

async function copyTextToClipboard(text: string) {
  if (!text) {
    return false;
  }
  try {
    if (navigator.clipboard?.writeText) {
      const didCopy = await Promise.race([
        navigator.clipboard.writeText(text).then(
          () => true,
          () => false,
        ),
        new Promise<boolean>((resolve) => {
          window.setTimeout(() => resolve(false), 300);
        }),
      ]);
      if (didCopy) {
        return true;
      }
    }
  } catch {
    // Fall back below when browser clipboard permission is denied.
  }

  const textArea = document.createElement("textarea");
  textArea.value = text;
  textArea.setAttribute("readonly", "");
  textArea.style.position = "fixed";
  textArea.style.top = "0";
  textArea.style.left = "-9999px";
  textArea.style.opacity = "0";
  document.body.appendChild(textArea);
  textArea.focus();
  textArea.select();
  try {
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    textArea.remove();
  }
}

function ConfirmKeyActionModal({
  pendingAction,
  isSubmitting,
  error,
  onClose,
  onConfirm,
}: {
  pendingAction: PendingAction;
  isSubmitting: boolean;
  error?: string | null;
  onClose: () => void;
  onConfirm: () => Promise<void>;
}) {
  const { msg } = useI18n();
  const destructive = pendingAction.action === "delete";
  const action = actionLabel(pendingAction.action, msg);
  const actionVerb = actionVerbLabel(pendingAction.action, msg);
  const keyName = pendingAction.apiKey.name || msg("apiKeys.thisKey", "this key");
  const title =
    pendingAction.action === "delete"
      ? msg("apiKeys.deleteTitle", "Delete API key")
      : msg("apiKeys.actionTitle", "{action} key?", { action });
  const body =
    pendingAction.action === "delete"
      ? msg("apiKeys.deleteBody", "Are you sure you want to delete {keyName}? This action can't be undone.", {
          keyName,
        })
      : msg("apiKeys.confirmBody", "Are you sure you want to {action} {keyName}?", {
          action: actionVerb,
          keyName,
        });
  const icon =
    pendingAction.action === "delete" ? (
      <Trash2 className="size-5" aria-hidden />
    ) : pendingAction.action === "disable" ? (
      <Ban className="size-5" aria-hidden />
    ) : (
      <Power className="size-5" aria-hidden />
    );

  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogMedia
            className={destructive ? "bg-destructive/10 text-destructive dark:bg-destructive/20" : undefined}
          >
            {icon}
          </AlertDialogMedia>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{body}</AlertDialogDescription>
        </AlertDialogHeader>
        {error ? <InlineError>{error}</InlineError> : null}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={isSubmitting}>{msg("common.cancel", "Cancel")}</AlertDialogCancel>
          <AlertDialogAction
            type="button"
            variant={destructive ? "destructive" : "default"}
            className="min-w-[82px]"
            onClick={() => void onConfirm().catch(() => undefined)}
            disabled={isSubmitting}
          >
            {isSubmitting ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
            {action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function InlineError({ children }: { children: ReactNode }) {
  return (
    <Alert variant="destructive" className="mt-4">
      <AlertCircle className="size-4 shrink-0" aria-hidden />
      <AlertDescription>{children}</AlertDescription>
    </Alert>
  );
}

function InfoTooltip({ label }: { label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={label}
            className="-m-1 text-muted-foreground hover:text-foreground"
          >
            <Info className="size-3.5" aria-hidden />
          </Button>
        }
      />
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function ApiKeysState({ text, tone = "muted" }: { text: string; tone?: "muted" | "error" }) {
  return (
    <TableRow className="border-border hover:bg-transparent">
      <TableCell colSpan={6} className="h-[156px] px-4 py-8 text-center text-sm text-muted-foreground">
        <div>
          {tone === "error" ? (
            <AlertCircle className="mx-auto mb-3 size-6 text-destructive" aria-hidden />
          ) : (
            <KeyRound className="mx-auto mb-3 size-6 text-muted-foreground/70" aria-hidden />
          )}
          {text}
        </div>
      </TableCell>
    </TableRow>
  );
}

function resolveWorkspace(routeWorkspaceId: string | undefined, workspaces: Workspace[], activeWorkspace: Workspace) {
  if (!routeWorkspaceId) {
    return activeWorkspace;
  }
  const known = workspaces.find((workspace) => workspace.id === routeWorkspaceId);
  if (known) {
    return known;
  }
  if (routeWorkspaceId === defaultWorkspace.id) {
    return defaultWorkspace;
  }
  return {
    ...defaultWorkspace,
    id: routeWorkspaceId,
    name: routeWorkspaceId,
  };
}

function keyHint(apiKey: WorkspaceApiKey) {
  if (apiKey.partial_key_hint) {
    return apiKey.partial_key_hint;
  }
  if (apiKey.key_prefix || apiKey.key_suffix) {
    return `${apiKey.key_prefix ?? "sk-ant"}...${apiKey.key_suffix ?? ""}`;
  }
  return "sk-ant-api03-...";
}

function formatDate(value?: string | null) {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat("en", {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(date);
}

function displayIdentity(account: { email_address?: string; display_name?: string; full_name?: string } | null) {
  const email = account?.email_address ?? "test@openmanagedagent.local";
  return {
    email,
    name: account?.display_name ?? account?.full_name ?? email.split("@")[0] ?? "test",
  };
}

function displayCreator(apiKey: WorkspaceApiKey, fallback: Identity) {
  const createdBy = apiKey.created_by;
  const email = createdBy?.email ?? fallback.email;
  return {
    email,
    name: createdBy?.name ?? email.split("@")[0] ?? fallback.name,
  };
}

function normalizeKeyStatus(apiKey: WorkspaceApiKey) {
  if (apiKey.archived_at) {
    return "archived";
  }
  const status = apiKey.status?.toLowerCase();
  if (status === "inactive" || status === "archived") {
    return status;
  }
  return "active";
}

function isArchivedKey(apiKey: WorkspaceApiKey) {
  return normalizeKeyStatus(apiKey) === "archived";
}

function statusForAction(action: KeyAction) {
  switch (action) {
    case "enable":
      return "active";
    case "disable":
      return "inactive";
    case "delete":
      return "archived";
  }
}

function actionLabel(action: KeyAction, msg: ReturnType<typeof useI18n>["msg"]) {
  switch (action) {
    case "enable":
      return msg("apiKeys.action.enable", "Enable");
    case "disable":
      return msg("apiKeys.action.disable", "Disable");
    case "delete":
      return msg("apiKeys.action.delete", "Delete");
  }
}

function actionVerbLabel(action: KeyAction, msg: ReturnType<typeof useI18n>["msg"]) {
  switch (action) {
    case "enable":
      return msg("apiKeys.verb.enable", "enable");
    case "disable":
      return msg("apiKeys.verb.disable", "disable");
    case "delete":
      return msg("apiKeys.verb.delete", "delete");
  }
}

function readableError(error: unknown) {
  if (!error) {
    return null;
  }
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "object" && error !== null && "message" in error) {
    const message = (error as { message?: unknown }).message;
    if (typeof message === "string") {
      return message;
    }
  }
  return "Request failed.";
}
