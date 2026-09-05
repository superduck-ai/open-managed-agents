import { useId, type ComponentProps } from 'react';
import { GitBranch, Trash2 } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Card, CardAction, CardContent, CardHeader, CardTitle } from '@/shared/ui/card';
import { Field, FieldDescription, FieldLabel } from '@/shared/ui/field';
import { Input } from '@/shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import type { GitRepositoryResourceFormValue } from '../types';
import { gitResourceMountPathValid, gitResourceURLValid } from './git-resource';

function GitInputField({ label, error, ...props }: ComponentProps<typeof Input> & { label: string; error?: string }) {
  const id = useId();
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input {...props} id={id} aria-invalid={Boolean(error)} aria-describedby={error ? `${id}-error` : undefined} />
      {error ? (
        <FieldDescription id={`${id}-error`} className="text-destructive">
          {error}
        </FieldDescription>
      ) : null}
    </Field>
  );
}

export function GitRepositoryFields({
  resource,
  index,
  onChange,
  onRemove,
}: {
  resource: GitRepositoryResourceFormValue;
  index: number;
  onChange: (resource: GitRepositoryResourceFormValue) => void;
  onRemove: () => void;
}) {
  const { msg } = useI18n();
  const checkoutId = useId();
  const patch = (value: Partial<GitRepositoryResourceFormValue>) => onChange({ ...resource, ...value });
  const checkoutLabels = {
    '': msg('managedAgents.git.checkout.none', 'None'),
    branch: msg('managedAgents.git.checkout.branch', 'Branch'),
    commit: msg('managedAgents.git.checkout.commit', 'Commit'),
  };
  return (
    <Card size="sm" className="mx-px gap-3 py-3">
      <CardHeader className="grid-cols-[1fr_auto] items-center px-3">
        <CardTitle className="flex items-center gap-2 text-sm">
          <GitBranch className="size-4 text-muted-foreground" aria-hidden />
          {msg('managedAgents.git.repository', 'GitHub repository')}
        </CardTitle>
        <CardAction>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={msg('managedAgents.git.remove', 'Remove GitHub repository {index}', { index: index + 1 })}
            onClick={onRemove}
          >
            <Trash2 aria-hidden />
          </Button>
        </CardAction>
      </CardHeader>
      <CardContent className="space-y-3 px-3">
        <GitInputField
          label={msg('managedAgents.git.url', 'URL')}
          type="url"
          required
          value={resource.url}
          placeholder="https://github.com/owner/repo"
          onChange={(event) => patch({ url: event.target.value })}
          error={
            resource.url && !gitResourceURLValid(resource.url)
              ? msg('managedAgents.git.urlError', 'Use https://github.com/owner/repo without .git or a trailing slash.')
              : undefined
          }
        />
        <GitInputField
          label={msg('managedAgents.git.optionalToken', 'Authorization token (optional for public repositories)')}
          type="password"
          autoComplete="new-password"
          value={resource.authorizationToken}
          placeholder="ghp_…"
          maxLength={8192}
          onChange={(event) => patch({ authorizationToken: event.target.value })}
        />
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor={checkoutId}>{msg('managedAgents.git.checkout', 'Checkout (optional)')}</FieldLabel>
            <Select
              value={resource.checkoutType}
              onValueChange={(value) => {
                if (value === '' || value === 'branch' || value === 'commit')
                  patch({ checkoutType: value, checkoutValue: '' });
              }}
            >
              <SelectTrigger id={checkoutId} className="w-full">
                <SelectValue>{checkoutLabels[resource.checkoutType]}</SelectValue>
              </SelectTrigger>
              <SelectContent>
                {Object.entries(checkoutLabels).map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
          {resource.checkoutType ? (
            <GitInputField
              required
              label={
                resource.checkoutType === 'commit'
                  ? msg('managedAgents.git.sha', 'Commit SHA')
                  : msg('managedAgents.git.branch', 'Branch name')
              }
              value={resource.checkoutValue}
              placeholder={resource.checkoutType === 'commit' ? 'Commit SHA' : 'main'}
              maxLength={resource.checkoutType === 'commit' ? 64 : 255}
              error={
                resource.checkoutType === 'commit' &&
                resource.checkoutValue &&
                !/^[a-fA-F0-9]{7,64}$/.test(resource.checkoutValue.trim())
                  ? msg('managedAgents.git.shaError', 'Enter a commit SHA with 7 to 64 hexadecimal characters.')
                  : undefined
              }
              onChange={(event) => patch({ checkoutValue: event.target.value })}
            />
          ) : null}
        </div>
        <GitInputField
          label={msg('managedAgents.git.mount', 'Mount path (optional)')}
          value={resource.mountPath}
          placeholder="/workspace/repo-name"
          onChange={(event) => patch({ mountPath: event.target.value })}
          error={
            !gitResourceMountPathValid(resource.mountPath)
              ? msg(
                  'managedAgents.git.mountError',
                  'Use a directory under /workspace, without relative or reserved path segments.',
                )
              : undefined
          }
        />
      </CardContent>
    </Card>
  );
}
