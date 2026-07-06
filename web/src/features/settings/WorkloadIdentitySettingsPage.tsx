import {
  Ban,
  Copy,
  Info,
  MoreVertical,
  Network,
  Plus,
  RotateCcw,
  Trash2,
} from "lucide-react";
import {
  useMemo,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import { useI18n } from "../../shared/i18n";
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
import { Alert, AlertDescription, AlertTitle } from "../../shared/ui/alert";
import { Badge } from "../../shared/ui/badge";
import { Button, ButtonLink } from "../../shared/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
} from "../../shared/ui/card";
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
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../../shared/ui/dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "../../shared/ui/empty";
import { Field, FieldDescription, FieldLabel } from "../../shared/ui/field";
import { Input } from "../../shared/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../../shared/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../../shared/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "../../shared/ui/tabs";
import { Textarea } from "../../shared/ui/textarea";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "../../shared/ui/tooltip";

type WorkloadIdentityStatus = "active" | "disabled";
type WorkloadIdentityProviderType =
  "aws" | "azure" | "gcp" | "github-actions" | "kubernetes";

type WorkloadIdentityRecord = {
  id: string;
  name: string;
  provider: WorkloadIdentityProviderType;
  issuer: string;
  audience: string;
  subjectPattern: string;
  createdAt: number;
  lastExchangeAt: number | null;
  status: WorkloadIdentityStatus;
};

type WorkloadIdentityConfirmation =
  | { kind: "disable"; record: WorkloadIdentityRecord }
  | { kind: "delete"; record: WorkloadIdentityRecord }
  | null;

const providerSeeds: Record<
  WorkloadIdentityProviderType,
  { issuer: string; audience: string; subjectPattern: string }
> = {
  aws: {
    issuer: "https://sts.amazonaws.com",
    audience: "claude-console://organization/default",
    subjectPattern: "arn:aws:sts::*:assumed-role/claude-*/*",
  },
  azure: {
    issuer: "https://login.microsoftonline.com/{tenant-id}/v2.0",
    audience: "api://claude-console/default",
    subjectPattern: "sub:system:serviceprincipal:{application-id}",
  },
  gcp: {
    issuer: "https://sts.googleapis.com",
    audience:
      "//iam.googleapis.com/projects/123456789/locations/global/workloadIdentityPools/claude/providers/default",
    subjectPattern: "attribute.repository/open-managed-agent/*",
  },
  "github-actions": {
    issuer: "https://token.actions.githubusercontent.com",
    audience: "claude-console://github-actions/default",
    subjectPattern: "repo:open-managed-agent/*:ref:refs/heads/main",
  },
  kubernetes: {
    issuer: "https://kubernetes.default.svc.cluster.local",
    audience: "claude-console://kubernetes/default",
    subjectPattern: "system:serviceaccount:production:claude-runner",
  },
};

export function WorkloadIdentitySettingsPage() {
  const { locale, msg } = useI18n();
  const nextIdRef = useRef(1);
  const [activeTab, setActiveTab] = useState<WorkloadIdentityStatus>("active");
  const [records, setRecords] = useState<WorkloadIdentityRecord[]>([]);
  const [createOpen, setCreateOpen] = useState(false);
  const [detailRecord, setDetailRecord] =
    useState<WorkloadIdentityRecord | null>(null);
  const [confirmation, setConfirmation] =
    useState<WorkloadIdentityConfirmation>(null);
  const [copiedRecordId, setCopiedRecordId] = useState<string | null>(null);
  const copiedResetRef = useRef<number | null>(null);
  const [draftName, setDraftName] = useState("");
  const [draftProvider, setDraftProvider] =
    useState<WorkloadIdentityProviderType>("aws");
  const [draftIssuer, setDraftIssuer] = useState(providerSeeds.aws.issuer);
  const [draftAudience, setDraftAudience] = useState(
    providerSeeds.aws.audience,
  );
  const [draftSubjectPattern, setDraftSubjectPattern] = useState(
    providerSeeds.aws.subjectPattern,
  );

  const providerOptions = useMemo(
    () =>
      [
        { value: "aws", label: msg("workloadIdentity.provider.aws", "AWS") },
        {
          value: "azure",
          label: msg("workloadIdentity.provider.azure", "Azure"),
        },
        {
          value: "gcp",
          label: msg("workloadIdentity.provider.gcp", "Google Cloud"),
        },
        {
          value: "github-actions",
          label: msg(
            "workloadIdentity.provider.githubActions",
            "GitHub Actions",
          ),
        },
        {
          value: "kubernetes",
          label: msg("workloadIdentity.provider.kubernetes", "Kubernetes"),
        },
      ] satisfies Array<{ value: WorkloadIdentityProviderType; label: string }>,
    [msg],
  );

  const visibleRecords = useMemo(
    () => records.filter((record) => record.status === activeTab),
    [activeTab, records],
  );

  const resetDraft = () => {
    setDraftName("");
    applyProviderPreset("aws");
  };

  const openCreateDialog = (nextOpen: boolean) => {
    setCreateOpen(nextOpen);
    if (nextOpen) {
      resetDraft();
    } else {
      resetDraft();
    }
  };

  const openDetailDialog = (record: WorkloadIdentityRecord | null) => {
    setDetailRecord(record);
    setCopiedRecordId(null);
    if (copiedResetRef.current !== null) {
      window.clearTimeout(copiedResetRef.current);
      copiedResetRef.current = null;
    }
  };

  const applyProviderPreset = (provider: WorkloadIdentityProviderType) => {
    const preset = providerSeeds[provider];
    setDraftProvider(provider);
    setDraftIssuer(preset.issuer);
    setDraftAudience(preset.audience);
    setDraftSubjectPattern(preset.subjectPattern);
  };

  const handleCreate = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedName = draftName.trim();
    if (!trimmedName) {
      return;
    }
    const sequence = nextIdRef.current++;
    const nextRecord: WorkloadIdentityRecord = {
      id: `wif_local_${String(sequence).padStart(4, "0")}`,
      name: trimmedName,
      provider: draftProvider,
      issuer: draftIssuer.trim(),
      audience: draftAudience.trim(),
      subjectPattern: draftSubjectPattern.trim(),
      createdAt: Date.now(),
      lastExchangeAt: null,
      status: "active",
    };
    setRecords((current) => [nextRecord, ...current]);
    setActiveTab("active");
    openCreateDialog(false);
  };

  const handleDisable = (record: WorkloadIdentityRecord) => {
    setRecords((current) =>
      current.map((item) =>
        item.id === record.id
          ? {
              ...item,
              status: "disabled",
            }
          : item,
      ),
    );
    setConfirmation(null);
  };

  const handleRestore = (record: WorkloadIdentityRecord) => {
    setRecords((current) =>
      current.map((item) =>
        item.id === record.id
          ? {
              ...item,
              status: "active",
            }
          : item,
      ),
    );
  };

  const handleDelete = (record: WorkloadIdentityRecord) => {
    setRecords((current) => current.filter((item) => item.id !== record.id));
    setConfirmation(null);
  };

  const copyAudience = async () => {
    if (!detailRecord) {
      return;
    }
    try {
      await navigator.clipboard?.writeText(detailRecord.audience);
      setCopiedRecordId(detailRecord.id);
      if (copiedResetRef.current !== null) {
        window.clearTimeout(copiedResetRef.current);
      }
      copiedResetRef.current = window.setTimeout(() => {
        setCopiedRecordId(null);
        copiedResetRef.current = null;
      }, 1600);
    } catch {
      setCopiedRecordId(null);
    }
  };

  const confirmationTitle =
    confirmation?.kind === "disable"
      ? msg("workloadIdentity.disableConfirm.title", "Disable provider?")
      : msg("workloadIdentity.deleteConfirm.title", "Delete preview row?");
  const confirmationBody =
    confirmation?.kind === "disable"
      ? msg(
          "workloadIdentity.disableConfirm.body",
          "Disable {name}? It will stop appearing in the active list and move to the disabled tab.",
          { name: confirmation?.record.name ?? "" },
        )
      : msg(
          "workloadIdentity.deleteConfirm.body",
          "Delete the local preview row for {name}? This only removes it from the settings demo.",
          { name: confirmation?.record.name ?? "" },
        );

  return (
    <TooltipProvider>
      <section
        className="mx-auto w-full max-w-[1100px] space-y-4"
        data-testid="settings-workload-identity-page"
      >
        <Card>
          <CardHeader className="space-y-3">
            <CardAction className="flex flex-wrap gap-2">
              <ButtonLink
                variant="outline"
                size="sm"
                href="/settings/service-accounts"
              >
                {msg(
                  "workloadIdentity.actions.viewServiceAccounts",
                  "View service accounts",
                )}
              </ButtonLink>
              <Button
                type="button"
                size="sm"
                onClick={() => openCreateDialog(true)}
              >
                <Plus className="size-4" aria-hidden />
                {msg("workloadIdentity.create", "Create provider")}
              </Button>
            </CardAction>
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-xl font-semibold tracking-normal text-foreground">
                {msg("nav.workloadIdentity", "Workload identity")}
              </h1>
              <Badge variant="secondary" className="rounded-full px-2.5 py-1">
                {msg("workloadIdentity.shortLived", "Short-lived access")}
              </Badge>
            </div>
            <CardDescription>
              {msg(
                "workloadIdentity.description",
                "Configure federation providers so CI systems and cloud workloads can exchange external identity tokens for Claude access without long-lived keys.",
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Alert>
              <Info className="size-4" aria-hidden />
              <AlertTitle>
                {msg(
                  "workloadIdentity.notice.title",
                  "Prefer federation over stored secrets",
                )}
              </AlertTitle>
              <AlertDescription>
                {msg(
                  "workloadIdentity.notice.body",
                  "Use workload identity providers for short-lived token exchange, then reserve admin keys and workspace API keys for the small set of flows that truly need them.",
                )}
              </AlertDescription>
            </Alert>

            <Tabs
              value={activeTab}
              onValueChange={(nextValue) =>
                nextValue && setActiveTab(nextValue as WorkloadIdentityStatus)
              }
              className="gap-4"
            >
              <TabsList>
                <TabsTrigger value="active">
                  {msg("common.active", "Active")}
                </TabsTrigger>
                <TabsTrigger value="disabled">
                  {msg("workloadIdentity.disabled", "Disabled")}
                </TabsTrigger>
              </TabsList>
              <TabsContent value="active">
                {visibleRecords.length ? (
                  <WorkloadIdentityTable
                    locale={locale}
                    records={visibleRecords}
                    onViewTrustPolicy={openDetailDialog}
                    onDisable={(record) =>
                      setConfirmation({ kind: "disable", record })
                    }
                    onRestore={handleRestore}
                    onDelete={(record) =>
                      setConfirmation({ kind: "delete", record })
                    }
                  />
                ) : (
                  <WorkloadIdentityEmptyState
                    icon={Network}
                    title={msg(
                      "workloadIdentity.empty.activeTitle",
                      "No providers yet",
                    )}
                    body={msg(
                      "workloadIdentity.empty.activeBody",
                      "Create a provider when workloads need to exchange cloud or CI identity for Claude access.",
                    )}
                    action={
                      <Button
                        type="button"
                        onClick={() => openCreateDialog(true)}
                      >
                        <Plus className="size-4" aria-hidden />
                        {msg(
                          "workloadIdentity.createFirst",
                          "Create first provider",
                        )}
                      </Button>
                    }
                  />
                )}
              </TabsContent>
              <TabsContent value="disabled">
                {visibleRecords.length ? (
                  <WorkloadIdentityTable
                    locale={locale}
                    records={visibleRecords}
                    onViewTrustPolicy={openDetailDialog}
                    onDisable={(record) =>
                      setConfirmation({ kind: "disable", record })
                    }
                    onRestore={handleRestore}
                    onDelete={(record) =>
                      setConfirmation({ kind: "delete", record })
                    }
                  />
                ) : (
                  <WorkloadIdentityEmptyState
                    icon={Ban}
                    title={msg(
                      "workloadIdentity.empty.disabledTitle",
                      "No disabled providers",
                    )}
                    body={msg(
                      "workloadIdentity.empty.disabledBody",
                      "Disabled providers appear here after you pause a trust policy.",
                    )}
                    action={
                      <Button
                        type="button"
                        variant="outline"
                        onClick={() => setActiveTab("active")}
                      >
                        {msg(
                          "workloadIdentity.showActive",
                          "Show active providers",
                        )}
                      </Button>
                    }
                  />
                )}
              </TabsContent>
            </Tabs>
          </CardContent>
        </Card>

      <Dialog open={createOpen} onOpenChange={openCreateDialog}>
        <DialogContent className="sm:max-w-[620px]">
          <DialogHeader>
            <DialogTitle>
              {msg("workloadIdentity.createDialog.title", "Create provider")}
            </DialogTitle>
            <DialogDescription>
              {msg(
                "workloadIdentity.createDialog.description",
                "Set up a local preview federation provider by naming the trust policy and the external identity claims it accepts.",
              )}
            </DialogDescription>
          </DialogHeader>
          <form className="space-y-5" onSubmit={handleCreate}>
            <Field className="gap-2">
              <FieldLabel htmlFor="workload-identity-provider">
                {msg("workloadIdentity.createDialog.providerLabel", "Provider")}
              </FieldLabel>
              <Select<WorkloadIdentityProviderType>
                value={draftProvider}
                items={providerOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    applyProviderPreset(nextValue);
                  }
                }}
              >
                <SelectTrigger
                  id="workload-identity-provider"
                  aria-label={msg(
                    "workloadIdentity.createDialog.providerLabel",
                    "Provider",
                  )}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {providerOptions.map((option) => (
                    <SelectItem
                      key={option.value}
                      value={option.value}
                      label={option.label}
                    >
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {msg(
                  "workloadIdentity.createDialog.providerHelp",
                  "Choose the external identity issuer your workloads already trust today.",
                )}
              </FieldDescription>
            </Field>

            <Field className="gap-2">
              <FieldLabel htmlFor="workload-identity-name">
                {msg("common.name", "Name")}
              </FieldLabel>
              <Input
                id="workload-identity-name"
                value={draftName}
                onChange={(event) => setDraftName(event.target.value)}
                placeholder={msg(
                  "workloadIdentity.createDialog.namePlaceholder",
                  "Production deploy federation",
                )}
              />
              <FieldDescription>
                {msg(
                  "workloadIdentity.createDialog.nameHelp",
                  "Use a short name that identifies the workload or automation path this provider is for.",
                )}
              </FieldDescription>
            </Field>

            <Field className="gap-2">
              <FieldLabel htmlFor="workload-identity-issuer">
                {msg("workloadIdentity.createDialog.issuerLabel", "Issuer URL")}
              </FieldLabel>
              <Input
                id="workload-identity-issuer"
                value={draftIssuer}
                onChange={(event) => setDraftIssuer(event.target.value)}
              />
              <FieldDescription>
                {msg(
                  "workloadIdentity.createDialog.issuerHelp",
                  "This issuer identifies the cloud, CI, or cluster identity system that mints upstream tokens.",
                )}
              </FieldDescription>
            </Field>

            <Field className="gap-2">
              <FieldLabel htmlFor="workload-identity-audience">
                {msg("workloadIdentity.createDialog.audienceLabel", "Audience")}
              </FieldLabel>
              <Input
                id="workload-identity-audience"
                value={draftAudience}
                onChange={(event) => setDraftAudience(event.target.value)}
                className="font-mono text-xs"
              />
              <FieldDescription>
                {msg(
                  "workloadIdentity.createDialog.audienceHelp",
                  "The audience claim is what your workloads request when they exchange tokens for Claude access.",
                )}
              </FieldDescription>
            </Field>

            <Field className="gap-2">
              <FieldLabel htmlFor="workload-identity-subject">
                {msg(
                  "workloadIdentity.createDialog.subjectLabel",
                  "Trusted subject",
                )}
              </FieldLabel>
              <Textarea
                id="workload-identity-subject"
                value={draftSubjectPattern}
                onChange={(event) => setDraftSubjectPattern(event.target.value)}
                className="min-h-[112px] resize-y font-mono text-xs"
                placeholder={msg(
                  "workloadIdentity.createDialog.subjectPlaceholder",
                  "repo:open-managed-agent/*:ref:refs/heads/main",
                )}
              />
              <FieldDescription>
                {msg(
                  "workloadIdentity.createDialog.subjectHelp",
                  "Limit exchange to the exact repository, role, or service account subjects that should receive Claude access.",
                )}
              </FieldDescription>
            </Field>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => openCreateDialog(false)}
              >
                {msg("common.cancel", "Cancel")}
              </Button>
              <Button type="submit" disabled={!draftName.trim()}>
                {msg("common.create", "Create")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={detailRecord !== null}
        onOpenChange={(nextOpen) => !nextOpen && openDetailDialog(null)}
      >
        <DialogContent className="sm:max-w-[640px]">
          <DialogHeader>
            <DialogTitle>
              {msg("workloadIdentity.trustDialog.title", "Trust policy")}
            </DialogTitle>
            <DialogDescription>
              {msg(
                "workloadIdentity.trustDialog.description",
                "Review the issuer, audience, and trusted subject pattern for {name}.",
                { name: detailRecord?.name ?? "" },
              )}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-5">
            <Field className="gap-2">
              <FieldLabel>
                {msg("workloadIdentity.trustDialog.provider", "Provider")}
              </FieldLabel>
              <Input
                value={
                  detailRecord ? providerLabel(detailRecord.provider, msg) : ""
                }
                readOnly
                aria-readonly="true"
              />
            </Field>
            <Field className="gap-2">
              <FieldLabel>
                {msg("workloadIdentity.trustDialog.issuer", "Issuer URL")}
              </FieldLabel>
              <Input
                value={detailRecord?.issuer ?? ""}
                readOnly
                aria-readonly="true"
                className="font-mono text-xs"
              />
            </Field>
            <Field className="gap-2">
              <FieldLabel>
                {msg("workloadIdentity.trustDialog.audience", "Audience")}
              </FieldLabel>
              <div className="flex gap-2">
                <Input
                  value={detailRecord?.audience ?? ""}
                  readOnly
                  aria-readonly="true"
                  className="font-mono text-xs"
                />
                <Button type="button" variant="outline" onClick={copyAudience}>
                  <Copy className="size-4" aria-hidden />
                  {copiedRecordId === detailRecord?.id
                    ? msg("common.copied", "Copied")
                    : msg("common.copy", "Copy")}
                </Button>
              </div>
            </Field>
            <Field className="gap-2">
              <FieldLabel>
                {msg("workloadIdentity.trustDialog.subject", "Trusted subject")}
              </FieldLabel>
              <Textarea
                value={detailRecord?.subjectPattern ?? ""}
                readOnly
                aria-readonly="true"
                className="min-h-[112px] resize-none font-mono text-xs"
              />
            </Field>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => openDetailDialog(null)}
            >
              {msg("common.close", "Close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

        <AlertDialog
          open={confirmation !== null}
          onOpenChange={(nextOpen) => !nextOpen && setConfirmation(null)}
        >
          <AlertDialogContent size="sm">
            <AlertDialogHeader>
              <AlertDialogMedia>
                {confirmation?.kind === "disable" ? (
                  <Ban aria-hidden />
                ) : (
                  <Trash2 aria-hidden />
                )}
              </AlertDialogMedia>
              <AlertDialogTitle>{confirmationTitle}</AlertDialogTitle>
              <AlertDialogDescription>{confirmationBody}</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel onClick={() => setConfirmation(null)}>
                {msg("common.cancel", "Cancel")}
              </AlertDialogCancel>
              <AlertDialogAction
                variant={
                  confirmation?.kind === "delete" ? "destructive" : "default"
                }
                onClick={() => {
                  if (!confirmation) {
                    return;
                  }
                  if (confirmation.kind === "disable") {
                    handleDisable(confirmation.record);
                    return;
                  }
                  handleDelete(confirmation.record);
                }}
              >
                {confirmation?.kind === "disable"
                  ? msg("workloadIdentity.disable", "Disable provider")
                  : msg("workloadIdentity.deletePreview", "Delete preview row")}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </section>
    </TooltipProvider>
  );
}

function WorkloadIdentityTable({
  locale,
  records,
  onViewTrustPolicy,
  onDisable,
  onRestore,
  onDelete,
}: {
  locale: string;
  records: WorkloadIdentityRecord[];
  onViewTrustPolicy: (record: WorkloadIdentityRecord) => void;
  onDisable: (record: WorkloadIdentityRecord) => void;
  onRestore: (record: WorkloadIdentityRecord) => void;
  onDelete: (record: WorkloadIdentityRecord) => void;
}) {
  const { msg } = useI18n();

  return (
    <Card className="overflow-hidden">
      <CardContent className="p-0">
        <Table
          aria-label={msg(
            "workloadIdentity.table.ariaLabel",
            "Workload identity providers",
          )}
        >
          <TableHeader className="text-muted-foreground">
            <TableRow className="hover:bg-transparent">
              <TableHead className="px-5 py-3">
                {msg("common.name", "Name")}
              </TableHead>
              <TableHead className="px-5 py-3">
                {msg("workloadIdentity.table.provider", "Provider")}
              </TableHead>
              <TableHead className="px-5 py-3">
                {msg("workloadIdentity.table.issuer", "Issuer")}
              </TableHead>
              <TableHead className="px-5 py-3">
                {msg("workloadIdentity.table.audience", "Audience")}
              </TableHead>
              <TableHead className="px-5 py-3">
                {msg("workloadIdentity.table.lastExchange", "Last exchange")}
              </TableHead>
              <TableHead className="px-5 py-3">
                {msg("workloadIdentity.table.status", "Status")}
              </TableHead>
              <TableHead className="w-14 px-5 py-3" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {records.map((record) => (
              <TableRow
                key={record.id}
                className="text-foreground last:border-0"
              >
                <TableCell className="px-5 py-4 align-top">
                  <div className="min-w-0 space-y-1">
                    <div className="font-medium text-foreground">
                      {record.name}
                    </div>
                    <p className="font-mono text-xs text-muted-foreground">
                      {record.id}
                    </p>
                  </div>
                </TableCell>
                <TableCell className="px-5 py-4 align-top">
                  <Badge variant="outline">
                    {providerLabel(record.provider, msg)}
                  </Badge>
                </TableCell>
                <TableCell className="w-[220px] max-w-[220px] px-5 py-4 align-top">
                  <WorkloadIdentityValuePreview value={record.issuer} />
                </TableCell>
                <TableCell className="w-[220px] max-w-[220px] px-5 py-4 align-top">
                  <WorkloadIdentityValuePreview value={record.audience} />
                </TableCell>
                <TableCell className="px-5 py-4 align-top text-sm text-muted-foreground">
                  {record.lastExchangeAt
                    ? formatDateTime(record.lastExchangeAt, locale)
                    : msg("workloadIdentity.table.neverExchanged", "Never")}
                </TableCell>
                <TableCell className="px-5 py-4 align-top">
                  <Badge
                    variant={
                      record.status === "active" ? "secondary" : "outline"
                    }
                  >
                    {record.status === "active"
                      ? msg("common.active", "Active")
                      : msg("workloadIdentity.disabled", "Disabled")}
                  </Badge>
                </TableCell>
                <TableCell className="px-5 py-4 text-right align-top">
                  <WorkloadIdentityActionsMenu
                    record={record}
                    onViewTrustPolicy={onViewTrustPolicy}
                    onDisable={onDisable}
                    onRestore={onRestore}
                    onDelete={onDelete}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

function WorkloadIdentityValuePreview({ value }: { value: string }) {
  return (
    <Tooltip>
      <TooltipTrigger>
        <span className="block w-[220px] max-w-[220px] cursor-help overflow-hidden font-mono text-xs text-muted-foreground">
          <span className="block truncate">{value}</span>
        </span>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm break-all font-mono text-[11px] leading-5">
        {value}
      </TooltipContent>
    </Tooltip>
  );
}

function WorkloadIdentityActionsMenu({
  record,
  onViewTrustPolicy,
  onDisable,
  onRestore,
  onDelete,
}: {
  record: WorkloadIdentityRecord;
  onViewTrustPolicy: (record: WorkloadIdentityRecord) => void;
  onDisable: (record: WorkloadIdentityRecord) => void;
  onRestore: (record: WorkloadIdentityRecord) => void;
  onDelete: (record: WorkloadIdentityRecord) => void;
}) {
  const { msg } = useI18n();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="text-muted-foreground"
            aria-label={msg(
              "workloadIdentity.moreActions",
              "More actions for {name}",
              {
                name: record.name,
              },
            )}
          />
        }
      >
        <MoreVertical className="size-4" aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuItem onClick={() => onViewTrustPolicy(record)}>
          <Network className="size-4" aria-hidden />
          <span>
            {msg("workloadIdentity.viewTrustPolicy", "View trust policy")}
          </span>
        </DropdownMenuItem>
        {record.status === "active" ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={() => onDisable(record)}>
              <Ban className="size-4" aria-hidden />
              <span>{msg("workloadIdentity.disable", "Disable provider")}</span>
            </DropdownMenuItem>
          </>
        ) : (
          <>
            <DropdownMenuItem onClick={() => onRestore(record)}>
              <RotateCcw className="size-4" aria-hidden />
              <span>{msg("workloadIdentity.restore", "Restore provider")}</span>
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              onClick={() => onDelete(record)}
            >
              <Trash2 className="size-4" aria-hidden />
              <span>
                {msg("workloadIdentity.deletePreview", "Delete preview row")}
              </span>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function WorkloadIdentityEmptyState({
  icon: Icon,
  title,
  body,
  action,
}: {
  icon: typeof Network;
  title: string;
  body: string;
  action?: ReactNode;
}) {
  return (
    <Empty className="min-h-[280px] rounded-lg border-border bg-card">
      <EmptyHeader>
        <EmptyMedia
          variant="icon"
          className="size-10 rounded-full border border-border bg-secondary text-muted-foreground"
        >
          <Icon className="size-5" aria-hidden />
        </EmptyMedia>
        <EmptyTitle>
          <h2 className="text-[20px] font-semibold leading-7 text-foreground">
            {title}
          </h2>
        </EmptyTitle>
        <EmptyDescription className="max-w-[520px]">{body}</EmptyDescription>
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  );
}

function providerLabel(
  provider: WorkloadIdentityProviderType,
  msg: (
    key: string,
    fallback: string,
    params?: Record<string, string | number>,
  ) => string,
) {
  switch (provider) {
    case "aws":
      return msg("workloadIdentity.provider.aws", "AWS");
    case "azure":
      return msg("workloadIdentity.provider.azure", "Azure");
    case "gcp":
      return msg("workloadIdentity.provider.gcp", "Google Cloud");
    case "github-actions":
      return msg("workloadIdentity.provider.githubActions", "GitHub Actions");
    case "kubernetes":
      return msg("workloadIdentity.provider.kubernetes", "Kubernetes");
    default:
      return provider;
  }
}

function formatDateTime(value: number, locale: string) {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(value);
}
