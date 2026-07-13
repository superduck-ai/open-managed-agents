import { Info, Shield } from "lucide-react";
import { useMemo, useState } from "react";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/shared/ui/field";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { Switch } from "@/shared/ui/switch";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/shared/ui/tooltip";
import { useI18n } from "../../shared/i18n";
import { ConsolePageFrame, SettingRow } from "./frame";

type RetentionWindow = "30-days" | "90-days" | "1-year" | "indefinite";
type ExportAccess = "admins" | "billing-and-admins" | "disabled";

export function PrivacyControlsPage() {
  const { msg } = useI18n();
  const [trainingExcluded, setTrainingExcluded] = useState(true);
  const [redactionEnabled, setRedactionEnabled] = useState(true);
  const [retentionWindow, setRetentionWindow] = useState<RetentionWindow>("90-days");
  const [retentionDraft, setRetentionDraft] = useState<RetentionWindow>("90-days");
  const [retentionOpen, setRetentionOpen] = useState(false);
  const [exportAccess, setExportAccess] = useState<ExportAccess>("admins");
  const [exportAccessDraft, setExportAccessDraft] = useState<ExportAccess>("admins");
  const [exportAccessOpen, setExportAccessOpen] = useState(false);

  const retentionOptions = useMemo(
    () =>
      [
        { value: "30-days", label: msg("privacyControls.retention.option30", "30 days") },
        { value: "90-days", label: msg("privacyControls.retention.option90", "90 days") },
        { value: "1-year", label: msg("privacyControls.retention.option365", "1 year") },
        { value: "indefinite", label: msg("privacyControls.retention.optionIndefinite", "Indefinite") },
      ] satisfies Array<{ value: RetentionWindow; label: string }>,
    [msg],
  );

  const exportAccessOptions = useMemo(
    () =>
      [
        { value: "admins", label: msg("privacyControls.exports.optionAdmins", "Admins only") },
        {
          value: "billing-and-admins",
          label: msg("privacyControls.exports.optionBilling", "Billing and admins"),
        },
        { value: "disabled", label: msg("privacyControls.exports.optionDisabled", "Disabled") },
      ] satisfies Array<{ value: ExportAccess; label: string }>,
    [msg],
  );

  const retentionLabel = retentionOptions.find((option) => option.value === retentionWindow)?.label ?? retentionWindow;
  const exportAccessLabel = exportAccessOptions.find((option) => option.value === exportAccess)?.label ?? exportAccess;

  const openRetentionDialog = (nextOpen: boolean) => {
    setRetentionOpen(nextOpen);
    if (nextOpen) {
      setRetentionDraft(retentionWindow);
    }
  };

  const openExportAccessDialog = (nextOpen: boolean) => {
    setExportAccessOpen(nextOpen);
    if (nextOpen) {
      setExportAccessDraft(exportAccess);
    }
  };

  return (
    <TooltipProvider>
      <ConsolePageFrame
        title={msg("featurePage.privacyControls.title", "Privacy controls")}
        icon={Shield}
        description={msg(
          "featurePage.privacyControls.description",
          "Configure privacy and data handling controls for your organization.",
        )}
      >
        <Card className="divide-y divide-border rounded-lg p-0">
          <SettingRow
            title={
              <PrivacySettingTitle
                title={msg("privacyControls.training.title", "Training data")}
                tooltip={msg(
                  "privacyControls.training.tooltip",
                  "Applies to new prompts, attachments, and outputs created after this default changes.",
                )}
              />
            }
            body={msg(
              "privacyControls.training.body",
              "Prevent prompts, attachments, and outputs from being retained for model training by default.",
            )}
            detail={
              trainingExcluded
                ? msg("privacyControls.training.disabledDetail", "Current default: Excluded from training")
                : msg("privacyControls.training.enabledDetail", "Current default: Eligible for training")
            }
            action={
              <Switch
                checked={trainingExcluded}
                aria-label={msg("privacyControls.training.title", "Training data")}
                onCheckedChange={(nextChecked) => setTrainingExcluded(Boolean(nextChecked))}
              />
            }
          />
          <SettingRow
            title={
              <PrivacySettingTitle
                title={msg("privacyControls.redaction.title", "Sensitive metadata redaction")}
                tooltip={msg(
                  "privacyControls.redaction.tooltip",
                  "Masks email addresses and bearer-like tokens in stored activity metadata.",
                )}
              />
            }
            body={msg(
              "privacyControls.redaction.body",
              "Automatically redact sensitive metadata before activity is stored in logs and traces.",
            )}
            detail={
              redactionEnabled
                ? msg("privacyControls.redaction.enabledDetail", "Current default: Automatic redaction on")
                : msg("privacyControls.redaction.disabledDetail", "Current default: Automatic redaction off")
            }
            action={
              <Switch
                checked={redactionEnabled}
                aria-label={msg("privacyControls.redaction.title", "Sensitive metadata redaction")}
                onCheckedChange={(nextChecked) => setRedactionEnabled(Boolean(nextChecked))}
              />
            }
          />
          <SettingRow
            title={msg("privacyControls.retention.title", "Activity retention")}
            body={msg(
              "privacyControls.retention.body",
              "Choose how long request logs, traces, and audit metadata remain available by default.",
            )}
            detail={msg("privacyControls.currentDefault", "Current default: {value}", { value: retentionLabel })}
            action={
              <Button type="button" variant="outline" size="lg" onClick={() => openRetentionDialog(true)}>
                {msg("common.configure", "Configure")}
              </Button>
            }
          />
          <SettingRow
            title={msg("privacyControls.exports.title", "Export access")}
            body={msg(
              "privacyControls.exports.body",
              "Control who can export conversation history and usage evidence from the console.",
            )}
            detail={msg("privacyControls.currentDefault", "Current default: {value}", { value: exportAccessLabel })}
            action={
              <Button type="button" variant="outline" size="lg" onClick={() => openExportAccessDialog(true)}>
                {msg("common.configure", "Configure")}
              </Button>
            }
          />
        </Card>

        <Dialog open={retentionOpen} onOpenChange={openRetentionDialog}>
          <DialogContent className="sm:max-w-[480px]">
            <DialogHeader>
              <DialogTitle>{msg("privacyControls.retention.dialogTitle", "Configure activity retention")}</DialogTitle>
              <DialogDescription>
                {msg(
                  "privacyControls.retention.dialogDescription",
                  "Choose how long request logs, traces, and audit metadata should remain available by default.",
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="privacy-controls-retention">
                {msg("privacyControls.retention.dialogLabel", "Retention window")}
              </FieldLabel>
              <Select<RetentionWindow>
                value={retentionDraft}
                items={retentionOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    setRetentionDraft(nextValue);
                  }
                }}
              >
                <SelectTrigger
                  id="privacy-controls-retention"
                  aria-label={msg("privacyControls.retention.dialogLabel", "Retention window")}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {retentionOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {msg(
                  "privacyControls.retention.dialogHelp",
                  "Shorter retention reduces how long activity history remains visible in the console.",
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openRetentionDialog(false)}>
                {msg("common.cancel", "Cancel")}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setRetentionWindow(retentionDraft);
                  setRetentionOpen(false);
                }}
              >
                {msg("common.save", "Save")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={exportAccessOpen} onOpenChange={openExportAccessDialog}>
          <DialogContent className="sm:max-w-[480px]">
            <DialogHeader>
              <DialogTitle>{msg("privacyControls.exports.dialogTitle", "Configure export access")}</DialogTitle>
              <DialogDescription>
                {msg(
                  "privacyControls.exports.dialogDescription",
                  "Choose which members can export conversation history and usage evidence from the console.",
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="privacy-controls-export-access">
                {msg("privacyControls.exports.dialogLabel", "Export access")}
              </FieldLabel>
              <Select<ExportAccess>
                value={exportAccessDraft}
                items={exportAccessOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    setExportAccessDraft(nextValue);
                  }
                }}
              >
                <SelectTrigger
                  id="privacy-controls-export-access"
                  aria-label={msg("privacyControls.exports.dialogLabel", "Export access")}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {exportAccessOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {msg(
                  "privacyControls.exports.dialogHelp",
                  "Use this default to limit who can export conversation and usage evidence from the organization console.",
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openExportAccessDialog(false)}>
                {msg("common.cancel", "Cancel")}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setExportAccess(exportAccessDraft);
                  setExportAccessOpen(false);
                }}
              >
                {msg("common.save", "Save")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </ConsolePageFrame>
    </TooltipProvider>
  );
}

function PrivacySettingTitle({ title, tooltip }: { title: string; tooltip: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span>{title}</span>
      <InfoTooltip label={tooltip} />
    </span>
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
