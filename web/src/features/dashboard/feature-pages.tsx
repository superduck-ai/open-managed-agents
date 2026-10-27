import { Activity, KeyRound, Plus, Tags, TerminalSquare } from 'lucide-react';
import { placeholderIcons } from '../../app/layout/navigation';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Card, CardContent, CardHeader } from '@/shared/ui/card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { Textarea } from '@/shared/ui/textarea';
import { ConsolePageFrame, ControlRow, DataTable, EmptyState, PanelCard, PrimaryAction } from './frame';
import { formatRole, titleize } from './model';

const workbenchModelOptions = [
  { value: 'sonnet-4-6', label: 'Claude Sonnet 4.6' },
  { value: 'opus-4-8', label: 'Claude Opus 4.8' },
  { value: 'haiku-4-5', label: 'Claude Haiku 4.5' },
];

export function ConsolePlaceholderPage({ section }: { section: string }) {
  return <ConsoleFeaturePage section={section} />;
}

export function WorkbenchPage() {
  return (
    <ConsolePageFrame
      title="Workbench"
      description="Test prompts against Claude models before moving them into production."
      actions={<PrimaryAction href="/settings/workspaces/default/keys" icon={KeyRound} label="Get API key" />}
    >
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <Card className="min-h-[500px] gap-0 rounded-lg p-4">
          <CardHeader className="flex flex-row items-center gap-2 border-b-[0.5px] border-border p-0 pb-3">
            <Select<string> defaultValue="sonnet-4-6" items={workbenchModelOptions}>
              <SelectTrigger aria-label="Model" className="min-w-48">
                <SelectValue />
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                {workbenchModelOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value} label={option.label}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button type="button" size="lg">
              Run
            </Button>
          </CardHeader>
          <CardContent className="p-0">
            <Textarea
              aria-label="Prompt"
              className="mt-4 min-h-[360px] resize-none border-0 bg-transparent px-0 py-0 text-sm leading-6 shadow-none placeholder:text-muted-foreground/70 focus-visible:border-transparent dark:bg-transparent"
              placeholder="Write a prompt..."
            />
          </CardContent>
        </Card>
        <PanelCard title="Parameters">
          <div className="space-y-4">
            <ControlRow label="Temperature" value="1" />
            <ControlRow label="Max tokens" value="1024" />
            <ControlRow label="Streaming" value="On" />
          </div>
        </PanelCard>
      </div>
    </ConsolePageFrame>
  );
}

export function PlaygroundPage() {
  const { msg } = useI18n();
  return (
    <ConsolePageFrame title={msg('playground.title', 'Playground')}>
      <EmptyState
        icon={TerminalSquare}
        title={msg('playground.unavailable', 'Playground is not connected to a model service.')}
      >
        <ButtonLink href="/workbench" variant="outline">
          {msg('playground.openWorkbench', 'Open Workbench')}
        </ButtonLink>
      </EmptyState>
    </ConsolePageFrame>
  );
}

export function MembersPage() {
  const { msg } = useI18n();
  const { account } = useAuth();
  const primaryMembership = account?.memberships?.[0];
  const displayName =
    account?.display_name ||
    account?.full_name ||
    account?.email_address?.split('@')[0] ||
    msg('members.currentUser', 'Current user');
  const emailAddress = account?.email_address || msg('members.unknownEmail', 'Unknown email');
  const role = formatRole(primaryMembership?.role, msg);

  return (
    <ConsolePageFrame
      title={msg('members.title', 'Members')}
      eyebrow="1"
      actions={<PrimaryAction href="/members/invite" icon={Plus} label={msg('members.invite', 'Invite')} />}
    >
      <DataTable
        columns={[msg('common.name', 'Name'), msg('members.email', 'Email'), msg('members.role', 'Role')]}
        rows={[[displayName, emailAddress, role]]}
      />
    </ConsolePageFrame>
  );
}

export function ClaudeCodeUsagePage() {
  const { msg } = useI18n();

  return (
    <ConsolePageFrame title={msg('claudeCode.usage.title', 'Claude Code usage')}>
      <EmptyState
        icon={TerminalSquare}
        title={msg('claudeCode.usage.unavailable', 'Claude Code usage is not connected.')}
      />
    </ConsolePageFrame>
  );
}

export function ClaudeCodeSettingsPage() {
  const { msg } = useI18n();
  return (
    <ConsolePageFrame title={msg('claudeCode.settings.title', 'Claude Code settings')}>
      <EmptyState
        icon={TerminalSquare}
        title={msg('claudeCode.settings.unavailable', 'Claude Code settings are not connected.')}
      >
        <ButtonLink href="/settings/members" variant="outline">
          {msg('claudeCode.settings.viewMembers', 'View members')}
        </ButtonLink>
      </EmptyState>
    </ConsolePageFrame>
  );
}

export function ConsoleFeaturePage({ section }: { section: string }) {
  const { msg } = useI18n();
  const title =
    section === 'tags'
      ? msg('featurePage.tags.title', 'Tags')
      : section === 'billing'
        ? msg('featurePage.billing.title', 'Billing')
        : titleize(section);
  const icon =
    section === 'tags' ? Tags : (placeholderIcons[`/${section}` as keyof typeof placeholderIcons] ?? Activity);
  const unavailable =
    section === 'tags'
      ? msg('featurePage.tags.unavailable', 'Tag management is not connected.')
      : section === 'billing'
        ? msg('featurePage.billing.unavailable', 'Billing information is not connected.')
        : msg('featurePage.unavailable', 'This service is not connected.');

  return (
    <ConsolePageFrame title={title}>
      <EmptyState icon={icon} title={unavailable} />
    </ConsolePageFrame>
  );
}
