import { AlertCircle, Box, Plus } from "lucide-react";
import { useState } from "react";
import { useI18n } from "../../shared/i18n";
import { CreateWorkspaceDialog } from "../../shared/workspaces/CreateWorkspaceDialog";
import {
  buildCreateWorkspaceInput,
  workspaceApiKeysPath,
  workspaceColor,
  workspaceWebhooksPath,
} from "../../shared/workspaces/presentation";
import { useWorkspace } from "../../shared/workspaces/context";
import { Alert, AlertDescription, AlertTitle } from "../../shared/ui/alert";
import { Badge } from "../../shared/ui/badge";
import { Button, ButtonLink } from "../../shared/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader } from "../../shared/ui/card";
import { Skeleton } from "../../shared/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../../shared/ui/table";

export function WorkspacesSettingsPage() {
  const { msg } = useI18n();
  const { orgUuid, workspaces, activeWorkspaceId, createWorkspace, error, isLoading, refreshWorkspaces } =
    useWorkspace();
  const [createOpen, setCreateOpen] = useState(false);
  const [retrying, setRetrying] = useState(false);

  const handleCreate = async (name: string, displayColor: string) => {
    await createWorkspace(buildCreateWorkspaceInput(name, displayColor));
  };

  const handleRetry = async () => {
    setRetrying(true);
    try {
      await refreshWorkspaces();
    } finally {
      setRetrying(false);
    }
  };

  return (
    <section className="mx-auto w-full max-w-[1100px]" data-testid="settings-workspaces-page">
      <Card>
        <CardHeader>
          {orgUuid ? (
            <CardAction>
              <CreateWorkspaceDialog
                open={createOpen}
                onOpenChange={setCreateOpen}
                onCreate={handleCreate}
                trigger={
                  <Button type="button">
                    <Plus className="size-4" aria-hidden />
                    {msg("workspace.create.title", "Create workspace")}
                  </Button>
                }
              />
            </CardAction>
          ) : null}
          <h1 className="text-xl font-semibold tracking-normal text-foreground">
            {msg("nav.workspaces", "Workspaces")}
          </h1>
          <CardDescription>
            {msg(
              "settings.workspaces.description",
              "Review workspace-specific API keys, webhooks, residency, and create new workspaces from one settings view.",
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {!orgUuid ? (
            <Alert>
              <AlertCircle className="size-4" aria-hidden />
              <AlertDescription>
                {msg(
                  "settings.workspaces.noOrganization",
                  "No organization is available for workspace management in this session.",
                )}
              </AlertDescription>
            </Alert>
          ) : error ? (
            <Alert variant="destructive">
              <AlertCircle className="size-4" aria-hidden />
              <AlertTitle>{msg("settings.workspaces.loadError", "Workspaces could not be loaded.")}</AlertTitle>
              <AlertDescription className="gap-3">
                <p>{readableError(error)}</p>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={retrying}
                  onClick={() => void handleRetry()}
                >
                  {retrying ? msg("common.loading", "Loading...") : msg("common.retry", "Retry")}
                </Button>
              </AlertDescription>
            </Alert>
          ) : isLoading ? (
            <div aria-label={msg("workspace.loading", "Loading workspaces...")} className="space-y-3">
              {Array.from({ length: 3 }).map((_, index) => (
                <div
                  key={index}
                  className="grid gap-3 rounded-lg border border-border p-4 md:grid-cols-[1.5fr_1fr_auto]"
                >
                  <div className="flex items-center gap-3">
                    <Skeleton className="size-9 rounded-md" />
                    <div className="space-y-2">
                      <Skeleton className="h-4 w-32" />
                      <Skeleton className="h-3 w-24" />
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Skeleton className="h-4 w-16" />
                    <Skeleton className="h-3 w-28" />
                  </div>
                  <div className="flex gap-2">
                    <Skeleton className="h-7 w-20" />
                    <Skeleton className="h-7 w-20" />
                  </div>
                </div>
              ))}
            </div>
          ) : workspaces.length > 0 ? (
            <div className="overflow-x-auto">
              <Table aria-label={msg("nav.workspaces", "Workspaces")} className="min-w-[780px]">
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead>{msg("settings.workspaces.workspace", "Workspace")}</TableHead>
                    <TableHead>{msg("settings.workspaces.residency", "Residency")}</TableHead>
                    <TableHead className="text-right">{msg("common.actions", "Actions")}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {workspaces.map((workspace) => (
                    <TableRow key={workspace.id}>
                      <TableCell className="min-w-0">
                        <div className="flex min-w-0 items-start gap-3">
                          <span className="grid size-9 shrink-0 place-items-center rounded-md border border-border bg-muted/40">
                            <Box className="size-4" style={{ color: workspaceColor(workspace) }} aria-hidden />
                          </span>
                          <div className="min-w-0">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="truncate font-medium text-foreground">{workspace.name}</span>
                              {workspace.id === activeWorkspaceId ? (
                                <Badge variant="secondary">{msg("settings.workspaces.current", "Current")}</Badge>
                              ) : null}
                            </div>
                            <div className="mt-1 truncate font-mono text-xs text-muted-foreground">{workspace.id}</div>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="text-sm text-foreground">
                          {geoLabel(workspace.data_residency?.workspace_geo)}
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {msg("settings.workspaces.defaultInference", "Default inference: {value}", {
                            value: geoLabel(workspace.data_residency?.default_inference_geo),
                          })}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex flex-wrap justify-end gap-2">
                          <ButtonLink variant="outline" size="sm" href={workspaceApiKeysPath(workspace.id)}>
                            {msg("nav.apiKeys", "API keys")}
                          </ButtonLink>
                          <ButtonLink variant="ghost" size="sm" href={workspaceWebhooksPath(workspace.id)}>
                            {msg("nav.webhooks", "Webhooks")}
                          </ButtonLink>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : (
            <Alert>
              <AlertCircle className="size-4" aria-hidden />
              <AlertDescription>
                {msg("settings.workspaces.empty", "No workspaces are available for this organization yet.")}
              </AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>
    </section>
  );
}

function geoLabel(value?: string | null) {
  if (!value) {
    return "US";
  }
  if (value.toLowerCase() === "global") {
    return "Global";
  }
  return value.toUpperCase();
}

function readableError(error: unknown) {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return "Open Managed Agents could not reach the workspace directory.";
}
