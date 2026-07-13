import { Eye, MailOpen, ReceiptText, type LucideIcon, UsersRound } from 'lucide-react';
import { useMemo, useState, type ReactNode } from 'react';
import { useI18n } from '../../shared/i18n';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import { Button, ButtonLink } from '../../shared/ui/button';
import { Card, CardAction, CardContent, CardDescription, CardHeader } from '../../shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../../shared/ui/dialog';
import { Field, FieldDescription, FieldLabel } from '../../shared/ui/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';

type BillingAudience = 'billing-members' | 'admins-and-billing' | 'all-members';
type BillingCadence = 'weekly' | 'monthly' | 'quarterly';

export function BillingSettingsPage() {
  const { msg } = useI18n();
  const audienceOptions = useMemo(
    () =>
      [
        {
          value: 'billing-members',
          label: msg('billing.option.billingMembers', 'Billing members only'),
        },
        {
          value: 'admins-and-billing',
          label: msg('billing.option.adminsAndBilling', 'Admins and billing members'),
        },
        {
          value: 'all-members',
          label: msg('billing.option.allMembers', 'All members'),
        },
      ] satisfies Array<{ value: BillingAudience; label: string }>,
    [msg],
  );
  const cadenceOptions = useMemo(
    () =>
      [
        { value: 'weekly', label: msg('billing.cadence.optionWeekly', 'Weekly') },
        { value: 'monthly', label: msg('billing.cadence.optionMonthly', 'Monthly') },
        { value: 'quarterly', label: msg('billing.cadence.optionQuarterly', 'Quarterly') },
      ] satisfies Array<{ value: BillingCadence; label: string }>,
    [msg],
  );

  const [invoiceAudience, setInvoiceAudience] = useState<BillingAudience>('admins-and-billing');
  const [invoiceAudienceDraft, setInvoiceAudienceDraft] = useState<BillingAudience>(invoiceAudience);
  const [invoiceDialogOpen, setInvoiceDialogOpen] = useState(false);
  const [costVisibility, setCostVisibility] = useState<BillingAudience>('billing-members');
  const [costVisibilityDraft, setCostVisibilityDraft] = useState<BillingAudience>(costVisibility);
  const [costVisibilityDialogOpen, setCostVisibilityDialogOpen] = useState(false);
  const [statementCadence, setStatementCadence] = useState<BillingCadence>('monthly');
  const [statementCadenceDraft, setStatementCadenceDraft] = useState<BillingCadence>(statementCadence);
  const [statementCadenceDialogOpen, setStatementCadenceDialogOpen] = useState(false);

  const invoiceAudienceLabel =
    audienceOptions.find((option) => option.value === invoiceAudience)?.label ?? invoiceAudience;
  const costVisibilityLabel =
    audienceOptions.find((option) => option.value === costVisibility)?.label ?? costVisibility;
  const statementCadenceLabel =
    cadenceOptions.find((option) => option.value === statementCadence)?.label ?? statementCadence;

  const openInvoiceDialog = (nextOpen: boolean) => {
    setInvoiceDialogOpen(nextOpen);
    if (nextOpen) {
      setInvoiceAudienceDraft(invoiceAudience);
    }
  };

  const openCostVisibilityDialog = (nextOpen: boolean) => {
    setCostVisibilityDialogOpen(nextOpen);
    if (nextOpen) {
      setCostVisibilityDraft(costVisibility);
    }
  };

  const openStatementCadenceDialog = (nextOpen: boolean) => {
    setStatementCadenceDialogOpen(nextOpen);
    if (nextOpen) {
      setStatementCadenceDraft(statementCadence);
    }
  };

  return (
    <section className="mx-auto w-full max-w-[1100px] space-y-4" data-testid="settings-billing-page">
      <Card>
        <CardHeader>
          <CardAction className="flex flex-wrap gap-2">
            <ButtonLink variant="outline" size="sm" href="/cost">
              {msg('billing.actions.viewCost', 'View cost dashboard')}
            </ButtonLink>
            <ButtonLink variant="outline" size="sm" href="/usage/limits">
              {msg('billing.actions.reviewLimits', 'Review rate limits')}
            </ButtonLink>
          </CardAction>
          <h1 className="text-xl font-semibold tracking-normal text-foreground">{msg('nav.billing', 'Billing')}</h1>
          <CardDescription>
            {msg(
              'billing.description',
              'Review billing contacts, invoice delivery, and spend visibility defaults for your organization.',
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <Alert>
            <ReceiptText className="size-4" aria-hidden />
            <AlertTitle>{msg('billing.notice.title', 'Billing defaults apply to future statements')}</AlertTitle>
            <AlertDescription>
              {msg(
                'billing.notice.body',
                'Use these defaults to decide who receives invoices, who gets billing digests, and who can review spend from the console.',
              )}
            </AlertDescription>
          </Alert>

          <Card className="overflow-hidden py-0">
            <CardContent className="p-0">
              <div className="divide-y divide-border">
                <BillingSettingRow
                  icon={UsersRound}
                  title={msg('billing.contacts.title', 'Billing contacts')}
                  body={msg(
                    'billing.contacts.body',
                    'Review which members receive invoices, payment reminders, and billing follow-up.',
                  )}
                  detail={msg('billing.contacts.detail', 'Current workflow: Managed from the members directory')}
                  action={
                    <ButtonLink variant="outline" size="sm" href="/settings/members">
                      {msg('common.manage', 'Manage')}
                    </ButtonLink>
                  }
                />
                <BillingSettingRow
                  icon={MailOpen}
                  title={msg('billing.invoice.title', 'Invoice delivery')}
                  body={msg(
                    'billing.invoice.body',
                    'Choose which members automatically receive invoice emails and payment notices.',
                  )}
                  detail={msg('billing.currentDefault', 'Current default: {value}', { value: invoiceAudienceLabel })}
                  action={
                    <Button type="button" variant="outline" size="sm" onClick={() => openInvoiceDialog(true)}>
                      {msg('common.configure', 'Configure')}
                    </Button>
                  }
                />
                <BillingSettingRow
                  icon={Eye}
                  title={msg('billing.visibility.title', 'Cost visibility')}
                  body={msg(
                    'billing.visibility.body',
                    'Decide who can open spend dashboards and review billing summaries across the console.',
                  )}
                  detail={msg('billing.currentDefault', 'Current default: {value}', { value: costVisibilityLabel })}
                  action={
                    <Button type="button" variant="outline" size="sm" onClick={() => openCostVisibilityDialog(true)}>
                      {msg('common.configure', 'Configure')}
                    </Button>
                  }
                />
                <BillingSettingRow
                  icon={ReceiptText}
                  title={msg('billing.cadence.title', 'Billing digest cadence')}
                  body={msg(
                    'billing.cadence.body',
                    'Set how often digest emails summarize invoice changes and spend updates.',
                  )}
                  detail={msg('billing.currentDefault', 'Current default: {value}', {
                    value: statementCadenceLabel,
                  })}
                  action={
                    <Button type="button" variant="outline" size="sm" onClick={() => openStatementCadenceDialog(true)}>
                      {msg('common.configure', 'Configure')}
                    </Button>
                  }
                />
              </div>
            </CardContent>
          </Card>
        </CardContent>
      </Card>
      <Dialog open={invoiceDialogOpen} onOpenChange={openInvoiceDialog}>
        <DialogContent className="sm:max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{msg('billing.invoice.dialogTitle', 'Configure invoice delivery')}</DialogTitle>
            <DialogDescription>
              {msg(
                'billing.invoice.dialogDescription',
                'Choose which members should receive invoice emails and payment notices by default.',
              )}
            </DialogDescription>
          </DialogHeader>
          <Field className="gap-2">
            <FieldLabel htmlFor="billing-invoice-recipients">
              {msg('billing.invoice.dialogLabel', 'Recipients')}
            </FieldLabel>
            <Select<BillingAudience>
              value={invoiceAudienceDraft}
              items={audienceOptions}
              onValueChange={(nextValue) => {
                if (nextValue !== null) {
                  setInvoiceAudienceDraft(nextValue);
                }
              }}
            >
              <SelectTrigger
                id="billing-invoice-recipients"
                aria-label={msg('billing.invoice.dialogLabel', 'Recipients')}
                className="w-full"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {audienceOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value} label={option.label}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              {msg(
                'billing.invoice.dialogHelp',
                'Members selected here receive future invoice and payment-notice emails for this organization.',
              )}
            </FieldDescription>
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => openInvoiceDialog(false)}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button
              type="button"
              onClick={() => {
                setInvoiceAudience(invoiceAudienceDraft);
                setInvoiceDialogOpen(false);
              }}
            >
              {msg('common.save', 'Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={costVisibilityDialogOpen} onOpenChange={openCostVisibilityDialog}>
        <DialogContent className="sm:max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{msg('billing.visibility.dialogTitle', 'Configure cost visibility')}</DialogTitle>
            <DialogDescription>
              {msg(
                'billing.visibility.dialogDescription',
                'Choose which members can review spend summaries and billing dashboards across the console.',
              )}
            </DialogDescription>
          </DialogHeader>
          <Field className="gap-2">
            <FieldLabel htmlFor="billing-cost-visibility">
              {msg('billing.visibility.dialogLabel', 'Visibility')}
            </FieldLabel>
            <Select<BillingAudience>
              value={costVisibilityDraft}
              items={audienceOptions}
              onValueChange={(nextValue) => {
                if (nextValue !== null) {
                  setCostVisibilityDraft(nextValue);
                }
              }}
            >
              <SelectTrigger
                id="billing-cost-visibility"
                aria-label={msg('billing.visibility.dialogLabel', 'Visibility')}
                className="w-full"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {audienceOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value} label={option.label}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              {msg(
                'billing.visibility.dialogHelp',
                'Use a narrower audience when cost dashboards and invoice summaries should stay limited to reviewers.',
              )}
            </FieldDescription>
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => openCostVisibilityDialog(false)}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button
              type="button"
              onClick={() => {
                setCostVisibility(costVisibilityDraft);
                setCostVisibilityDialogOpen(false);
              }}
            >
              {msg('common.save', 'Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog open={statementCadenceDialogOpen} onOpenChange={openStatementCadenceDialog}>
        <DialogContent className="sm:max-w-[520px]">
          <DialogHeader>
            <DialogTitle>{msg('billing.cadence.dialogTitle', 'Configure billing digest cadence')}</DialogTitle>
            <DialogDescription>
              {msg(
                'billing.cadence.dialogDescription',
                'Choose how often billing digests summarize recent statement and spend changes.',
              )}
            </DialogDescription>
          </DialogHeader>
          <Field className="gap-2">
            <FieldLabel htmlFor="billing-digest-cadence">
              {msg('billing.cadence.dialogLabel', 'Digest cadence')}
            </FieldLabel>
            <Select<BillingCadence>
              value={statementCadenceDraft}
              items={cadenceOptions}
              onValueChange={(nextValue) => {
                if (nextValue !== null) {
                  setStatementCadenceDraft(nextValue);
                }
              }}
            >
              <SelectTrigger
                id="billing-digest-cadence"
                aria-label={msg('billing.cadence.dialogLabel', 'Digest cadence')}
                className="w-full"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {cadenceOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value} label={option.label}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>
              {msg(
                'billing.cadence.dialogHelp',
                'Digest cadence changes the summary emails sent to billing contacts; invoices are still delivered immediately.',
              )}
            </FieldDescription>
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => openStatementCadenceDialog(false)}>
              {msg('common.cancel', 'Cancel')}
            </Button>
            <Button
              type="button"
              onClick={() => {
                setStatementCadence(statementCadenceDraft);
                setStatementCadenceDialogOpen(false);
              }}
            >
              {msg('common.save', 'Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}

function BillingSettingRow({
  icon: Icon,
  title,
  body,
  detail,
  action,
}: {
  icon: LucideIcon;
  title: string;
  body: string;
  detail?: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-4 p-5 sm:flex-row sm:items-start sm:justify-between">
      <div className="flex min-w-0 items-start gap-3">
        <span className="mt-0.5 grid size-9 shrink-0 place-items-center rounded-md border border-border bg-muted/40 text-muted-foreground">
          <Icon className="size-4" aria-hidden />
        </span>
        <div className="min-w-0">
          <h2 className="text-sm font-semibold text-foreground">{title}</h2>
          <p className="mt-1 text-sm leading-6 text-muted-foreground">{body}</p>
          {detail ? <div className="mt-2 text-sm font-medium text-foreground">{detail}</div> : null}
        </div>
      </div>
      {action ? <div className="shrink-0">{action}</div> : null}
    </div>
  );
}
