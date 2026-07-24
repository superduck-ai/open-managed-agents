import {
  Activity,
  Code2,
  Gauge,
  KeyRound,
  LifeBuoy,
  MessageSquare,
  Network,
  Plus,
  Tags,
  TerminalSquare,
} from 'lucide-react';
import { useState } from 'react';
import { placeholderIcons } from '../../app/layout/navigation';
import { useAuth } from '../../shared/auth/context';
import { useI18n } from '../../shared/i18n';
import { useWorkspace } from '../../shared/workspaces/context';
import { Button, ButtonLink } from '@/shared/ui/button';
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/shared/ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/shared/ui/dialog';
import { Field, FieldDescription, FieldLabel } from '@/shared/ui/field';
import { InputGroup, InputGroupButton, InputGroupInput } from '@/shared/ui/input-group';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { Textarea } from '@/shared/ui/textarea';
import { useModelCatalog } from '../model-catalog/hooks';
import { isCatalogModelID, modelCatalogDisplayName } from '../model-catalog/model';
import {
  ConsolePageFrame,
  ControlRow,
  DataTable,
  EmptyState,
  MetricTile,
  NoticeCard,
  PanelCard,
  PrimaryAction,
  SecondaryAction,
  SettingRow,
} from './frame';
import { formatRole, titleize, type IconComponent } from './model';

export function ConsolePlaceholderPage({ section }: { section: string }) {
  return <ConsoleFeaturePage section={section} />;
}

export function WorkbenchPage() {
  return (
    <ConsolePageFrame
      title="Workbench"
      icon={Code2}
      description="Test prompts against available models before moving them into production."
      actions={<PrimaryAction href="/settings/workspaces/default/keys" icon={KeyRound} label="Get API key" />}
    >
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
        <Card className="min-h-[500px] gap-0 rounded-lg p-4">
          <CardHeader className="flex flex-row items-center gap-2 border-b border-border p-0 pb-3">
            <CatalogModelSelect label="Model" triggerClassName="min-w-48" />
            <Button type="button" size="lg">
              Run
            </Button>
          </CardHeader>
          <CardContent className="p-0">
            <Textarea
              aria-label="Prompt"
              className="mt-4 min-h-[360px] resize-none border-0 bg-transparent px-0 py-0 text-sm leading-6 shadow-none placeholder:text-muted-foreground/70 focus-visible:border-transparent focus-visible:ring-0 dark:bg-transparent"
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
    <ConsolePageFrame
      title={msg('playground.title', 'Playground')}
      icon={Code2}
      description={msg('playground.description', 'Preview model behavior with a chat-style prompt runner.')}
      eyebrow={msg('nav.preview', 'Preview')}
    >
      <div className="grid min-h-[560px] gap-4 xl:grid-cols-[280px_minmax(0,1fr)]">
        <PanelCard title={msg('playground.configuration', 'Configuration')}>
          <div className="space-y-3">
            <Field className="gap-1">
              <FieldLabel className="text-xs font-medium text-muted-foreground">
                {msg('analytics.table.model', 'Model')}
              </FieldLabel>
              <CatalogModelSelect label={msg('analytics.table.model', 'Model')} triggerClassName="w-full" />
            </Field>
            <ControlRow
              label={msg('playground.systemPrompt', 'System prompt')}
              value={msg('playground.none', 'None')}
            />
            <ControlRow label={msg('playground.tools', 'Tools')} value={msg('playground.disabled', 'Disabled')} />
          </div>
        </PanelCard>
        <Card className="flex flex-col gap-0 rounded-lg p-0">
          <CardHeader className="border-b border-border px-4 py-3">
            <CardTitle className="text-sm font-semibold text-foreground">
              {msg('playground.messages', 'Messages')}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex flex-1 items-center justify-center px-6 text-center text-sm text-muted-foreground">
            {msg('playground.empty', "Start a conversation to preview the selected model's response.")}
          </CardContent>
          <CardFooter className="border-t border-border bg-transparent p-3">
            <InputGroup className="h-auto rounded-lg px-3 py-2 shadow-none">
              <InputGroupInput
                aria-label={msg('playground.message.label', 'Message')}
                className="h-8 text-sm placeholder:text-muted-foreground/70"
                placeholder={msg('playground.message.placeholder', 'Message the selected model...')}
              />
              <InputGroupButton type="button" size="sm" variant="default" className="shrink-0">
                {msg('playground.send', 'Send')}
              </InputGroupButton>
            </InputGroup>
          </CardFooter>
        </Card>
      </div>
    </ConsolePageFrame>
  );
}

function CatalogModelSelect({ label, triggerClassName }: { label: string; triggerClassName?: string }) {
  const { orgUuid } = useWorkspace();
  const modelCatalog = useModelCatalog(orgUuid);
  const [selectedModelID, setSelectedModelID] = useState('');
  const currentModelID = isCatalogModelID(selectedModelID, modelCatalog.models)
    ? selectedModelID
    : modelCatalog.defaultModelID;
  const options = modelCatalog.models.map((model) => ({
    value: model.model_name,
    label: modelCatalogDisplayName(model),
  }));
  const placeholder = modelCatalog.isPending
    ? 'Loading models'
    : modelCatalog.isError
      ? 'Models unavailable'
      : 'Select a model';

  return (
    <Select<string>
      value={currentModelID || null}
      items={options}
      disabled={!options.length}
      onValueChange={(nextModelID) => setSelectedModelID(nextModelID ?? '')}
    >
      <SelectTrigger aria-label={label} className={triggerClassName}>
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        {options.map((option) => (
          <SelectItem key={option.value} value={option.value} label={option.label}>
            {option.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

export function LimitsPage() {
  const { msg } = useI18n();

  return (
    <ConsolePageFrame
      title={msg('limits.title', 'Rate limits')}
      icon={Gauge}
      description={msg(
        'limits.description',
        'Limits help us mitigate against misuse and manage API capacity. Rate limits restrict API usage frequency over a certain period of time.',
      )}
      eyebrow={msg('limits.customPlan', 'Custom Plan')}
      actions={
        <SecondaryAction
          href="/usage/limits"
          icon={Gauge}
          label={msg('limits.monitorUsage', 'Monitor rate limit usage')}
        />
      }
    >
      <NoticeCard
        icon={LifeBuoy}
        title={msg(
          'limits.notice.title',
          'Contact the Anthropic accounts team to learn more about custom rate limits.',
        )}
        action={
          <ButtonLink
            href="https://www.anthropic.com/contact-sales"
            target="_blank"
            rel="noreferrer"
            variant="outline"
            size="lg"
          >
            {msg('limits.notice.action', 'Contact sales')}
          </ButtonLink>
        }
      />
      <DataTable
        columns={[
          msg('analytics.table.model', 'Model'),
          msg('analytics.rateLimits.requestsPerMinute', 'Requests per Minute'),
          msg('analytics.rateLimits.inputTokensPerMinute', 'Input Tokens per Minute'),
          msg('analytics.rateLimits.outputTokensPerMinute', 'Output Tokens per Minute'),
        ]}
        rows={limitRows}
      />
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
      icon={MessageSquare}
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
    <ConsolePageFrame title={msg('claudeCode.usage.title', 'Claude Code usage')} icon={TerminalSquare}>
      <div className="grid gap-3 md:grid-cols-3">
        <MetricTile title={msg('claudeCode.usage.seats', 'Seats')} value="0" />
        <MetricTile title={msg('claudeCode.usage.activeUsers', 'Active users')} value="0" />
        <MetricTile title={msg('claudeCode.usage.tokenUsage', 'Token usage')} value="0" />
      </div>
      <EmptyState
        icon={TerminalSquare}
        title={msg('claudeCode.usage.empty.title', 'No Claude Code usage yet')}
        body={msg('claudeCode.usage.empty.body', 'Usage appears here after seats start using Claude Code.')}
      />
    </ConsolePageFrame>
  );
}

export function ClaudeCodeSettingsPage() {
  const { msg } = useI18n();
  const workspacePermissionOptions = [
    {
      value: 'seat-holders',
      label: msg('claudeCode.settings.workspacePermissions.optionSeatHolders', 'Members with a Claude Code seat'),
    },
    {
      value: 'developers-and-admins',
      label: msg('claudeCode.settings.workspacePermissions.optionDevelopers', 'Developers and admins'),
    },
    {
      value: 'disabled',
      label: msg('claudeCode.settings.workspacePermissions.optionDisabled', 'Disabled by default'),
    },
  ];
  const [workspacePermission, setWorkspacePermission] = useState('seat-holders');
  const [workspacePermissionDraft, setWorkspacePermissionDraft] = useState(workspacePermission);
  const [workspacePermissionsOpen, setWorkspacePermissionsOpen] = useState(false);
  const selectedWorkspacePermissionLabel =
    workspacePermissionOptions.find((option) => option.value === workspacePermission)?.label ?? workspacePermission;
  const openWorkspacePermissionsDialog = (nextOpen: boolean) => {
    setWorkspacePermissionsOpen(nextOpen);
    if (nextOpen) {
      setWorkspacePermissionDraft(workspacePermission);
    }
  };

  return (
    <ConsolePageFrame title={msg('claudeCode.settings.title', 'Claude Code settings')} icon={TerminalSquare}>
      <Card className="divide-y divide-border rounded-lg p-0">
        <SettingRow
          title={msg('claudeCode.settings.seatAccess.title', 'Seat access')}
          body={msg('claudeCode.settings.seatAccess.body', 'Manage which members can use Claude Code.')}
          detail={msg('claudeCode.settings.seatAccess.detail', 'Claude Code seat access is managed from member roles.')}
          action={
            <ButtonLink href="/settings/members" variant="outline" size="lg">
              {msg('common.manage', 'Manage')}
            </ButtonLink>
          }
        />
        <Dialog open={workspacePermissionsOpen} onOpenChange={openWorkspacePermissionsDialog}>
          <SettingRow
            title={msg('claudeCode.settings.workspacePermissions.title', 'Workspace permissions')}
            body={msg(
              'claudeCode.settings.workspacePermissions.body',
              'Configure default access for Claude Code workspaces.',
            )}
            detail={msg('claudeCode.settings.workspacePermissions.current', 'Current default: {value}', {
              value: selectedWorkspacePermissionLabel,
            })}
            action={
              <DialogTrigger render={<Button type="button" variant="outline" size="lg" />}>
                {msg('common.configure', 'Configure')}
              </DialogTrigger>
            }
          />
          <DialogContent className="sm:max-w-[480px]">
            <DialogHeader>
              <DialogTitle>
                {msg('claudeCode.settings.workspacePermissions.dialogTitle', 'Configure workspace permissions')}
              </DialogTitle>
              <DialogDescription>
                {msg(
                  'claudeCode.settings.workspacePermissions.dialogBody',
                  'Choose the default Claude Code access level for newly configured workspaces.',
                )}
              </DialogDescription>
            </DialogHeader>
            <Field className="gap-2">
              <FieldLabel htmlFor="claude-code-workspace-permissions">
                {msg('claudeCode.settings.workspacePermissions.defaultAccess', 'Default access')}
              </FieldLabel>
              <Select<string>
                value={workspacePermissionDraft}
                items={workspacePermissionOptions}
                onValueChange={(nextValue) => {
                  if (nextValue !== null) {
                    setWorkspacePermissionDraft(nextValue);
                  }
                }}
              >
                <SelectTrigger
                  id="claude-code-workspace-permissions"
                  aria-label={msg('claudeCode.settings.workspacePermissions.defaultAccess', 'Default access')}
                  className="w-full"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  {workspacePermissionOptions.map((option) => (
                    <SelectItem key={option.value} value={option.value} label={option.label}>
                      {option.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>
                {msg(
                  'claudeCode.settings.workspacePermissions.help',
                  'This default applies when new Claude Code workspaces are enabled.',
                )}
              </FieldDescription>
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setWorkspacePermissionsOpen(false)}>
                {msg('common.cancel', 'Cancel')}
              </Button>
              <Button
                type="button"
                onClick={() => {
                  setWorkspacePermission(workspacePermissionDraft);
                  setWorkspacePermissionsOpen(false);
                }}
              >
                {msg('common.save', 'Save')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </Card>
    </ConsolePageFrame>
  );
}

export function ConsoleFeaturePage({ section }: { section: string }) {
  const { msg } = useI18n();
  const fallbackTitle = titleize(section);
  const config = featurePageConfig[section] ?? {
    title: fallbackTitle,
    icon: placeholderIcons[`/${section}` as keyof typeof placeholderIcons] ?? Activity,
    description: 'Manage this Open Managed Agents resource for the active workspace.',
    emptyTitle: `No ${fallbackTitle.toLowerCase()} configured`,
    emptyBody: 'New resources and configuration changes appear here.',
  };
  const copy = featurePageCopy(section, config, fallbackTitle, msg);

  return (
    <ConsolePageFrame title={copy.title} icon={config.icon} description={copy.description} eyebrow={copy.eyebrow}>
      <EmptyState icon={config.icon} title={copy.emptyTitle} body={copy.emptyBody} action={copy.action} />
    </ConsolePageFrame>
  );
}

const limitRows = [
  ['Claude Fable 5', '5', '10K excluding cache reads', '4K'],
  ['Claude Opus 4.8', '5', '10K excluding cache reads', '4K'],
  ['Claude Sonnet 4.6', '50', '40K excluding cache reads', '16K'],
  ['Claude Haiku 4.5', '50', '50K excluding cache reads', '20K'],
];

const featurePageConfig: Record<
  string,
  {
    title: string;
    icon: IconComponent;
    description: string;
    emptyTitle: string;
    emptyBody: string;
    eyebrow?: string;
    action?: string;
  }
> = {
  'mcp-tunnels': {
    title: 'MCP tunnels',
    icon: Network,
    description: 'Expose local MCP servers to managed agent sessions.',
    emptyTitle: 'No MCP tunnels',
    emptyBody: 'Preview tunnels and connection status appear here.',
    eyebrow: 'Preview',
  },
  tags: {
    title: 'Tags',
    icon: Tags,
    description: 'Organize resources with tags across workspaces.',
    emptyTitle: 'No tags',
    emptyBody: 'Create tags to classify resources and filter operational views.',
    action: 'Create tag',
  },
};

function featurePageCopy(
  section: string,
  config: {
    title: string;
    description: string;
    emptyTitle: string;
    emptyBody: string;
    eyebrow?: string;
    action?: string;
  },
  fallbackTitle: string,
  msg: ReturnType<typeof useI18n>['msg'],
) {
  switch (section) {
    case 'mcp-tunnels':
      return {
        title: msg('featurePage.mcpTunnels.title', config.title),
        description: msg('featurePage.mcpTunnels.description', config.description),
        emptyTitle: msg('featurePage.mcpTunnels.emptyTitle', config.emptyTitle),
        emptyBody: msg('featurePage.mcpTunnels.emptyBody', config.emptyBody),
        eyebrow: msg('nav.preview', config.eyebrow ?? 'Preview'),
        action: config.action,
      };
    case 'tags':
      return {
        title: msg('featurePage.tags.title', config.title),
        description: msg('featurePage.tags.description', config.description),
        emptyTitle: msg('featurePage.tags.emptyTitle', config.emptyTitle),
        emptyBody: msg('featurePage.tags.emptyBody', config.emptyBody),
        eyebrow: config.eyebrow,
        action: msg('featurePage.tags.action', config.action ?? 'Create tag'),
      };
    default:
      return {
        title: fallbackTitle,
        description: msg('featurePage.default.description', config.description),
        emptyTitle: msg('featurePage.default.emptyTitle', config.emptyTitle, { title: fallbackTitle.toLowerCase() }),
        emptyBody: msg('featurePage.default.emptyBody', config.emptyBody),
        eyebrow: config.eyebrow,
        action: config.action,
      };
  }
}
