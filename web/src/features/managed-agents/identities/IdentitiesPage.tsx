import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Input } from '../../../shared/ui/input';
import { useWorkspace } from '../../../shared/workspaces/context';
import { createIdentity, listIdentities, setIdentityEnabled } from './api';

export function IdentitiesPage() {
  const { msg } = useI18n();
  const { orgUuid, activeWorkspaceId } = useWorkspace();
  const client = useQueryClient();
  const [namespace, setNamespace] = useState('default');
  const [subjectID, setSubjectID] = useState('');
  const [displayName, setDisplayName] = useState('');
  const enabled = Boolean(orgUuid && activeWorkspaceId);
  const query = useQuery({
    queryKey: ['identities', orgUuid, activeWorkspaceId],
    enabled,
    queryFn: () => listIdentities(orgUuid ?? '', activeWorkspaceId),
  });
  const refresh = () => client.invalidateQueries({ queryKey: ['identities', orgUuid, activeWorkspaceId] });
  const create = useMutation({
    mutationFn: () =>
      createIdentity(orgUuid ?? '', activeWorkspaceId, {
        namespace,
        external_subject_id: subjectID,
        display_name: displayName,
      }),
    onSuccess: () => {
      setSubjectID('');
      setDisplayName('');
      void refresh();
    },
  });
  const toggle = useMutation({
    mutationFn: (identity: { id: string; enabled: boolean }) =>
      setIdentityEnabled(orgUuid ?? '', activeWorkspaceId, identity.id, !identity.enabled),
    onSuccess: () => void refresh(),
  });
  const rows = query.data?.data ?? [];
  return (
    <section className="mx-auto flex w-full max-w-5xl flex-col gap-6 p-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-medium">{msg('nav.identities', '执行身份')}</h1>
        <p className="text-sm text-muted-foreground">
          {msg('managedAgents.identities.summary', '业务身份决定谁可以执行，以及配置和凭据如何隔离。')}
        </p>
      </header>
      <form
        className="grid gap-3 rounded-lg border p-4 md:grid-cols-4"
        onSubmit={(event) => {
          event.preventDefault();
          create.mutate();
        }}
      >
        <Input
          value={namespace}
          onChange={(event) => setNamespace(event.target.value)}
          aria-label={msg('managedAgents.identities.namespace', '命名空间')}
        />
        <Input
          value={subjectID}
          onChange={(event) => setSubjectID(event.target.value)}
          aria-label={msg('managedAgents.identities.subject', '业务主体')}
        />
        <Input
          value={displayName}
          onChange={(event) => setDisplayName(event.target.value)}
          aria-label={msg('managedAgents.identities.displayName', '展示名')}
        />
        <Button
          type="submit"
          disabled={!enabled || create.isPending || subjectID.trim() === '' || displayName.trim() === ''}
        >
          {msg('managedAgents.identities.create', '创建身份')}
        </Button>
      </form>
      {query.isError ? (
        <p className="text-sm text-destructive">
          {msg('managedAgents.identities.loadError', '执行身份暂时无法加载。')}
        </p>
      ) : null}
      <ul className="divide-y rounded-lg border">
        {rows.length === 0 ? (
          <li className="p-4 text-sm text-muted-foreground">
            {msg('managedAgents.identities.empty', '还没有执行身份。')}
          </li>
        ) : null}
        {rows.map((identity) => (
          <li key={identity.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
            <div>
              <div className="font-medium">{identity.display_name}</div>
              <div className="font-mono text-xs text-muted-foreground">
                {identity.id} · {identity.namespace}/{identity.external_subject_id}
              </div>
            </div>
            <Button variant="outline" disabled={toggle.isPending} onClick={() => toggle.mutate(identity)}>
              {identity.enabled
                ? msg('managedAgents.identities.disable', '停用')
                : msg('managedAgents.identities.enable', '启用')}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}
