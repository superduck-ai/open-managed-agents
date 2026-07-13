import { Activity, Info, Shield } from "lucide-react";
import { useMemo, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/shared/ui/alert";
import { Button, ButtonLink } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/shared/ui/field";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { Switch } from "@/shared/ui/switch";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/shared/ui/tooltip";
import { useI18n } from "../../shared/i18n";
import { ConsolePageFrame, SecondaryAction, SettingRow } from "./frame";

type ReauthenticationWindow = "12-hours" | "24-hours" | "7-days";
type SecurityActivityVisibility = "admins" | "admins-and-billing" | "all-members";

export function SecurityPage() {
  const { msg } = useI18n();
  const [adminMfaRequired, setAdminMfaRequired] = useState(true);
  const [reauthenticationWindow, setReauthenticationWindow] = useState<ReauthenticationWindow>("24-hours");
  const [reauthenticationDraft, setReauthenticationDraft] = useState<ReauthenticationWindow>("24-hours");
  const [reauthenticationOpen, setReauthenticationOpen] = useState(false);
  const [securityActivityVisibility, setSecurityActivityVisibility] = useState<SecurityActivityVisibility>("admins");
  const [securityActivityVisibilityDraft, setSecurityActivityVisibilityDraft] =
    useState<SecurityActivityVisibility>("admins");
  const [securityActivityVisibilityOpen, setSecurityActivityVisibilityOpen] = useState(false);

  const reauthenticationOptions = useMemo(
    () =>
      [
        { value: "12-hours", label: msg("security.reauth.option12h", "Every 12 hours") },
        { value: "24-hours", label: msg("security.reauth.option24h", "Every 24 hours") },
        { value: "7-days", label: msg("security.reauth.option7d", "Every 7 days") },
      ] satisfies Array<{ value: ReauthenticationWindow; label: string }>,
    [msg],
  );

  const securityActivityVisibilityOptions = useMemo(
    () =>
      [
        { value: "admins", label: msg("security.visibility.optionAdmins", "Admins only") },
        {
          value: "admins-and-billing",
          label: msg("security.visibility.optionBilling", "Admins and billing"),
        },
        { value: "all-members", label: msg("security.visibility.optionAllMembers", "All members") },
      ] satisfies Array<{ value: SecurityActivityVisibility; label: string }>,
    [msg],
  );

  const reauthenticationLabel =
    reauthenticationOptions.find((option) => option.value === reauthenticationWindow)?.label ?? reauthenticationWindow;
  const securityActivityVisibilityLabel =
    securityActivityVisibilityOptions.find((option) => option.value === securityActivityVisibility)?.label ??
    securityActivityVisibility;

  const openReauthenticationDialog = (nextOpen: boolean) => {
    setReauthenticationOpen(nextOpen);
    if (nextOpen) {
      setReauthenticationDraft(reauthenticationWindow);
    }
  };

  const openSecurityActivityVisibilityDialog = (nextOpen: boolean) => {
    setSecurityActivityVisibilityOpen(nextOpen);
    if (nextOpen) {
      setSecurityActivityVisibilityDraft(securityActivityVisibility);
    }
  };

  return (
    <TooltipProvider>
      <ConsolePageFrame
        title={msg("featurePage.security.title", "Security")}
        icon={Shield}
        description={msg(
          "featurePage.security.description",
          "Configure security defaults, access reviews, and security activity visibility for your organization.",
        )}
        actions={<SecondaryAction href="/logs" icon={Activity} label={msg("security.actions.viewLogs", "View logs")} />}
      >
        <Alert>
          <Info className="size-4" aria-hidden />
          <AlertTitle>{msg("security.notice.title", "Security defaults apply to new console activity")}</AlertTitle>
          <AlertDescription>
            {msg(
              "security.notice.body",
              "Use these defaults to decide who needs stronger authentication and who can inspect security-sensitive console activity.",
            )}
          </AlertDescription>
        </Alert>

        <Card className="divide-y divide-border rounded-lg p-0">
          <SettingRow
            title={
              <SecuritySettingTitle
                title={msg("security.mfa.title", "Admin multi-factor authentication")}
                tooltip={msg("security.mfa.tooltip", "Applies the next time an admin or billing member signs in.")}
              />
            }
            body={msg(
              "security.mfa.body",
              "Require members with elevated permissions to verify with an additional factor before they can manage organization settings.",
            )}
            detail={
              adminMfaRequired
                ? msg("security.mfa.enabledDetail", "Current default: Required for admins and billing members")
                : msg("security.mfa.disabledDetail", "Current default: Optional for admins and billing members")
            }
            action={
              <Switch
                checked={adminMfaRequired}
                aria-label={msg("security.mfa.title", "Admin multi-factor authentication")}
                onCheckedChange={(nextChecked) => setAdminMfaRequired(Boolean(nextChecked))}
              />
            }
          />
          <SettingRow
            title={msg("security.members.title", "Member invitations")}
            body={msg(
              "security.members.body",
              "Review organization access, resend invitations, and remove members when access requirements change.",
            )}
            detail={msg("security.members.detail", "Current workflow: Managed from the members directory")}
            action={
              <ButtonLink href="/members" variant="outline" size="lg">
                {msg("common.manage", "Manage")}
              </ButtonLink>
            }
          />
          <SettingRow
            title={msg("security.reauth.title", "Session reauthentication")}
            body={msg(
              "security.reauth.body",
              "Ask members to reverify before reopening the console after an idle period.",
            )}
            detail={msg("security.currentDefault", "Current default: {value}", { value: reauthenticationLabel })}
            action={
              <Button type="button" variant="outline" size="lg" onClick={() => openReauthenticationDialog(true)}>
                {msg("common.configure", "Configure")}
              </Button>
            }
          />
          <SettingRow
            title={msg("security.visibility.title", "Security activity visibility")}
            body={msg(
              "security.visibility.body",
              "Choose who can view audit-oriented logs, rate-limit diagnostics, and incident history in the console.",
            )}
            detail={msg("security.currentDefault", "Current default: {value}", {
              value: securityActivityVisibilityLabel,
            })}
            action={
              <Button
                type="button"
                variant="outline"
                size="lg"
                onClick={() => openSecurityActivityVisibilityDialog(true)}
              >
                {msg("common.configure", "Configure")}
              </Button>
            }
          />
        </Card>

        <Dialog open={reauthenticationOpen} onOpenChange={openReauthenticationDialog}>
          <DialogContent className="sm:max-w-[480px]">
            <DialogHeader>
              <DialogTitle>{msg("security.reauth.dialogTitle", "Configure session reauthentication")}</DialogTitle>
              <DialogDescription>
                {msg(
                  "security.reauth.dialogDescription",
                  "Choose how often members should verify again before reopening the organization console.",
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="security-reauthentication-window">
                {msg("security.reauth.dialogLabel", "Reauthentication window")}
              </FieldLabel>
              <Select<ReauthenticationWindow>
                value={reauthenticationDraft}
                items={reauthenticationOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    setReauthenticationDraft(nextValue);
                  }
                }}
              >
                <SelectTrigger
                  id="security-reauthentication-window"
                  aria-label={msg("security.reauth.dialogLabel", "Reauthentication window")}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {reauthenticationOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {msg(
                  "security.reauth.dialogHelp",
                  "Shorter windows reduce how long an unattended console session can stay available.",
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openReauthenticationDialog(false)}>
                {msg("common.cancel", "Cancel")}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setReauthenticationWindow(reauthenticationDraft);
                  setReauthenticationOpen(false);
                }}
              >
                {msg("common.save", "Save")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={securityActivityVisibilityOpen} onOpenChange={openSecurityActivityVisibilityDialog}>
          <DialogContent className="sm:max-w-[480px]">
            <DialogHeader>
              <DialogTitle>
                {msg("security.visibility.dialogTitle", "Configure security activity visibility")}
              </DialogTitle>
              <DialogDescription>
                {msg(
                  "security.visibility.dialogDescription",
                  "Choose which members can inspect security-focused activity across the console.",
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="security-activity-visibility">
                {msg("security.visibility.dialogLabel", "Visibility")}
              </FieldLabel>
              <Select<SecurityActivityVisibility>
                value={securityActivityVisibilityDraft}
                items={securityActivityVisibilityOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    setSecurityActivityVisibilityDraft(nextValue);
                  }
                }}
              >
                <SelectTrigger
                  id="security-activity-visibility"
                  aria-label={msg("security.visibility.dialogLabel", "Visibility")}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {securityActivityVisibilityOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {msg(
                  "security.visibility.dialogHelp",
                  "Use a narrower audience when security logs and diagnostics should stay limited to reviewers.",
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openSecurityActivityVisibilityDialog(false)}>
                {msg("common.cancel", "Cancel")}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setSecurityActivityVisibility(securityActivityVisibilityDraft);
                  setSecurityActivityVisibilityOpen(false);
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

function SecuritySettingTitle({ title, tooltip }: { title: string; tooltip: string }) {
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
