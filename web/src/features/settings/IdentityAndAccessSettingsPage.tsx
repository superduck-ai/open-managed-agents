import { Info, ShieldCheck } from "lucide-react";
import { useMemo, useState } from "react";
import { useI18n } from "../../shared/i18n";
import { Alert, AlertDescription, AlertTitle } from "../../shared/ui/alert";
import { Button, ButtonLink } from "../../shared/ui/button";
import { Card, CardContent, CardDescription, CardHeader } from "../../shared/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../../shared/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "../../shared/ui/field";
import { Input } from "../../shared/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "../../shared/ui/select";
import { Switch } from "../../shared/ui/switch";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "../../shared/ui/tooltip";
import { SettingRow } from "../dashboard/frame";

type SignInMode = "email-link" | "sso-and-email" | "sso-only";
type DefaultInviteRole = "member" | "billing" | "admin";

export function IdentityAndAccessSettingsPage() {
  const { msg } = useI18n();
  const signInModeOptions = useMemo(
    () =>
      [
        {
          value: "email-link",
          label: msg("identityAccess.signIn.optionEmailLink", "Email verification links"),
        },
        {
          value: "sso-and-email",
          label: msg("identityAccess.signIn.optionSsoAndEmail", "SSO and email verification"),
        },
        {
          value: "sso-only",
          label: msg("identityAccess.signIn.optionSsoOnly", "SSO only"),
        },
      ] satisfies Array<{ value: SignInMode; label: string }>,
    [msg],
  );
  const inviteRoleOptions = useMemo(
    () =>
      [
        {
          value: "member",
          label: msg("identityAccess.roles.optionMember", "Member"),
        },
        {
          value: "billing",
          label: msg("identityAccess.roles.optionBilling", "Billing"),
        },
        {
          value: "admin",
          label: msg("identityAccess.roles.optionAdmin", "Admin"),
        },
      ] satisfies Array<{ value: DefaultInviteRole; label: string }>,
    [msg],
  );

  const [signInMode, setSignInMode] = useState<SignInMode>("sso-and-email");
  const [signInModeDraft, setSignInModeDraft] = useState<SignInMode>(signInMode);
  const [signInDialogOpen, setSignInDialogOpen] = useState(false);
  const [verifiedDomain, setVerifiedDomain] = useState("acme.dev");
  const [verifiedDomainDraft, setVerifiedDomainDraft] = useState(verifiedDomain);
  const [verifiedDomainDialogOpen, setVerifiedDomainDialogOpen] = useState(false);
  const [verifiedDomainsRequired, setVerifiedDomainsRequired] = useState(true);
  const [jitProvisioningEnabled, setJitProvisioningEnabled] = useState(true);
  const [defaultInviteRole, setDefaultInviteRole] = useState<DefaultInviteRole>("member");
  const [defaultInviteRoleDraft, setDefaultInviteRoleDraft] = useState<DefaultInviteRole>(defaultInviteRole);
  const [defaultInviteRoleDialogOpen, setDefaultInviteRoleDialogOpen] = useState(false);

  const signInModeLabel = signInModeOptions.find((option) => option.value === signInMode)?.label ?? signInMode;
  const defaultInviteRoleLabel =
    inviteRoleOptions.find((option) => option.value === defaultInviteRole)?.label ?? defaultInviteRole;

  const openSignInDialog = (nextOpen: boolean) => {
    setSignInDialogOpen(nextOpen);
    if (nextOpen) {
      setSignInModeDraft(signInMode);
    }
  };

  const openVerifiedDomainDialog = (nextOpen: boolean) => {
    setVerifiedDomainDialogOpen(nextOpen);
    if (nextOpen) {
      setVerifiedDomainDraft(verifiedDomain);
    }
  };

  const openDefaultInviteRoleDialog = (nextOpen: boolean) => {
    setDefaultInviteRoleDialogOpen(nextOpen);
    if (nextOpen) {
      setDefaultInviteRoleDraft(defaultInviteRole);
    }
  };

  return (
    <TooltipProvider>
      <section className="mx-auto w-full max-w-[1100px] space-y-4" data-testid="settings-identity-and-access-page">
        <Card>
          <CardHeader>
            <h1 className="text-xl font-semibold tracking-normal text-foreground">
              {msg("nav.identityAndAccess", "Identity and access")}
            </h1>
            <CardDescription>
              {msg(
                "identityAccess.description",
                "Configure sign-in, invitation defaults, and domain-based access policies for your organization.",
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Alert>
              <Info className="size-4" aria-hidden />
              <AlertTitle>
                {msg("identityAccess.notice.title", "Identity defaults apply to future member access")}
              </AlertTitle>
              <AlertDescription>
                {msg(
                  "identityAccess.notice.body",
                  "Use these settings to decide how members sign in, when domains are enforced, and what access level new invitations should receive by default.",
                )}
              </AlertDescription>
            </Alert>

            <Card className="overflow-hidden py-0">
              <CardContent className="p-0">
                <div className="divide-y divide-border">
                  <SettingRow
                    title={msg("identityAccess.directory.title", "Member directory")}
                    body={msg(
                      "identityAccess.directory.body",
                      "Review members, resend invitations, and remove access when identity requirements change.",
                    )}
                    detail={msg(
                      "identityAccess.directory.detail",
                      "Current workflow: Managed from the members directory",
                    )}
                    action={
                      <ButtonLink variant="outline" size="sm" href="/settings/members">
                        {msg("common.manage", "Manage")}
                      </ButtonLink>
                    }
                  />
                  <SettingRow
                    title={
                      <IdentitySettingTitle
                        title={msg("identityAccess.signIn.title", "Sign-in method")}
                        tooltip={msg(
                          "identityAccess.signIn.tooltip",
                          "Choose whether members authenticate with email verification, your SSO provider, or both.",
                        )}
                      />
                    }
                    body={msg(
                      "identityAccess.signIn.body",
                      "Decide how members authenticate when they open the organization console for the first time.",
                    )}
                    detail={msg("identityAccess.currentDefault", "Current default: {value}", {
                      value: signInModeLabel,
                    })}
                    action={
                      <Button type="button" variant="outline" size="sm" onClick={() => openSignInDialog(true)}>
                        {msg("common.configure", "Configure")}
                      </Button>
                    }
                  />
                  <SettingRow
                    title={
                      <IdentitySettingTitle
                        title={msg("identityAccess.domain.title", "Verified domain")}
                        tooltip={msg(
                          "identityAccess.domain.tooltip",
                          "Use a verified domain to route member invitations and SSO sign-in toward the expected identity boundary.",
                        )}
                      />
                    }
                    body={msg(
                      "identityAccess.domain.body",
                      "Choose the primary domain used for member invitations, just-in-time provisioning, and sign-in guidance.",
                    )}
                    detail={msg("identityAccess.currentDefault", "Current default: {value}", { value: verifiedDomain })}
                    action={
                      <Button type="button" variant="outline" size="sm" onClick={() => openVerifiedDomainDialog(true)}>
                        {msg("common.configure", "Configure")}
                      </Button>
                    }
                  />
                  <SettingRow
                    title={
                      <IdentitySettingTitle
                        title={msg("identityAccess.domainRestriction.title", "Restrict invites to verified domains")}
                        tooltip={msg(
                          "identityAccess.domainRestriction.tooltip",
                          "Applies to future invitations and self-serve sign-in attempts after this default changes.",
                        )}
                      />
                    }
                    body={msg(
                      "identityAccess.domainRestriction.body",
                      "Require invited members to use the verified domain before they can join this organization.",
                    )}
                    detail={
                      verifiedDomainsRequired
                        ? msg(
                            "identityAccess.domainRestriction.enabledDetail",
                            "Current default: Verified domains required",
                          )
                        : msg(
                            "identityAccess.domainRestriction.disabledDetail",
                            "Current default: Any domain can be invited",
                          )
                    }
                    action={
                      <Switch
                        checked={verifiedDomainsRequired}
                        aria-label={msg(
                          "identityAccess.domainRestriction.title",
                          "Restrict invites to verified domains",
                        )}
                        onCheckedChange={(nextChecked) => setVerifiedDomainsRequired(Boolean(nextChecked))}
                      />
                    }
                  />
                  <SettingRow
                    title={
                      <IdentitySettingTitle
                        title={msg("identityAccess.jit.title", "Just-in-time provisioning")}
                        tooltip={msg(
                          "identityAccess.jit.tooltip",
                          "Automatically creates a member record after a successful sign-in from an approved identity provider.",
                        )}
                      />
                    }
                    body={msg(
                      "identityAccess.jit.body",
                      "Create member access automatically after a successful first sign-in instead of requiring a manual invitation every time.",
                    )}
                    detail={
                      jitProvisioningEnabled
                        ? msg("identityAccess.jit.enabledDetail", "Current default: Automatic provisioning on")
                        : msg("identityAccess.jit.disabledDetail", "Current default: Manual invitations only")
                    }
                    action={
                      <Switch
                        checked={jitProvisioningEnabled}
                        aria-label={msg("identityAccess.jit.title", "Just-in-time provisioning")}
                        onCheckedChange={(nextChecked) => setJitProvisioningEnabled(Boolean(nextChecked))}
                      />
                    }
                  />
                  <SettingRow
                    title={msg("identityAccess.roles.title", "Default invite role")}
                    body={msg(
                      "identityAccess.roles.body",
                      "Choose the access level new member invitations should receive before an admin reviews them.",
                    )}
                    detail={msg("identityAccess.currentDefault", "Current default: {value}", {
                      value: defaultInviteRoleLabel,
                    })}
                    action={
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => openDefaultInviteRoleDialog(true)}
                      >
                        {msg("common.configure", "Configure")}
                      </Button>
                    }
                  />
                </div>
              </CardContent>
            </Card>
          </CardContent>
        </Card>

        <Dialog open={signInDialogOpen} onOpenChange={openSignInDialog}>
          <DialogContent className="sm:max-w-[520px]">
            <DialogHeader>
              <DialogTitle>{msg("identityAccess.signIn.dialogTitle", "Configure sign-in method")}</DialogTitle>
              <DialogDescription>
                {msg(
                  "identityAccess.signIn.dialogDescription",
                  "Choose how members should authenticate when opening the organization console.",
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="identity-access-sign-in-mode">
                {msg("identityAccess.signIn.dialogLabel", "Authentication mode")}
              </FieldLabel>
              <Select<SignInMode>
                value={signInModeDraft}
                items={signInModeOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    setSignInModeDraft(nextValue);
                  }
                }}
              >
                <SelectTrigger
                  id="identity-access-sign-in-mode"
                  aria-label={msg("identityAccess.signIn.dialogLabel", "Authentication mode")}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {signInModeOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {msg(
                  "identityAccess.signIn.dialogHelp",
                  "Use SSO-only when every member should authenticate through the same identity provider.",
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openSignInDialog(false)}>
                {msg("common.cancel", "Cancel")}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setSignInMode(signInModeDraft);
                  setSignInDialogOpen(false);
                }}
              >
                {msg("common.save", "Save")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={verifiedDomainDialogOpen} onOpenChange={openVerifiedDomainDialog}>
          <DialogContent className="sm:max-w-[520px]">
            <DialogHeader>
              <DialogTitle>{msg("identityAccess.domain.dialogTitle", "Configure verified domain")}</DialogTitle>
              <DialogDescription>
                {msg(
                  "identityAccess.domain.dialogDescription",
                  "Choose the primary domain that member invitations and sign-in guidance should use by default.",
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="identity-access-verified-domain">
                {msg("identityAccess.domain.dialogLabel", "Verified domain")}
              </FieldLabel>
              <Input
                id="identity-access-verified-domain"
                aria-label={msg("identityAccess.domain.dialogLabel", "Verified domain")}
                value={verifiedDomainDraft}
                onChange={(event) => setVerifiedDomainDraft(event.target.value)}
              />
              <FieldDescription>
                {msg(
                  "identityAccess.domain.dialogHelp",
                  "Use the domain members expect to use when signing in so invitations and JIT access stay aligned.",
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openVerifiedDomainDialog(false)}>
                {msg("common.cancel", "Cancel")}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  const nextDomain = verifiedDomainDraft.trim() || verifiedDomain;
                  setVerifiedDomain(nextDomain);
                  setVerifiedDomainDialogOpen(false);
                }}
              >
                {msg("common.save", "Save")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={defaultInviteRoleDialogOpen} onOpenChange={openDefaultInviteRoleDialog}>
          <DialogContent className="sm:max-w-[520px]">
            <DialogHeader>
              <DialogTitle>{msg("identityAccess.roles.dialogTitle", "Configure default invite role")}</DialogTitle>
              <DialogDescription>
                {msg(
                  "identityAccess.roles.dialogDescription",
                  "Choose which role new invitations should receive before a reviewer adjusts their access.",
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="identity-access-default-role">
                {msg("identityAccess.roles.dialogLabel", "Default role")}
              </FieldLabel>
              <Select<DefaultInviteRole>
                value={defaultInviteRoleDraft}
                items={inviteRoleOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    setDefaultInviteRoleDraft(nextValue);
                  }
                }}
              >
                <SelectTrigger
                  id="identity-access-default-role"
                  aria-label={msg("identityAccess.roles.dialogLabel", "Default role")}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {inviteRoleOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {msg(
                  "identityAccess.roles.dialogHelp",
                  "Use Member by default unless invitations should immediately land with billing or admin-level access.",
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => openDefaultInviteRoleDialog(false)}>
                {msg("common.cancel", "Cancel")}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setDefaultInviteRole(defaultInviteRoleDraft);
                  setDefaultInviteRoleDialogOpen(false);
                }}
              >
                {msg("common.save", "Save")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </section>
    </TooltipProvider>
  );
}

function IdentitySettingTitle({ title, tooltip }: { title: string; tooltip: string }) {
  return (
    <span className="inline-flex items-center gap-2">
      {title}
      <Tooltip>
        <TooltipTrigger
          aria-label={title}
          className="inline-flex text-muted-foreground transition hover:text-foreground"
        >
          <ShieldCheck className="size-4" aria-hidden />
          <span className="sr-only">{title}</span>
        </TooltipTrigger>
        <TooltipContent className="max-w-64 text-pretty">{tooltip}</TooltipContent>
      </Tooltip>
    </span>
  );
}
