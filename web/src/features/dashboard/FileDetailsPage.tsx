import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useState } from 'react';
import { ArrowLeft, Download } from 'lucide-react';
import { anthropicBetaApi } from '../../shared/api/anthropic';
import { useWorkspace } from '../../shared/workspaces/context';
import { useFormatters, useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { downloadFile, formatBytes, errorMessage, type ConsoleFile } from './model';

export function FileDetailsPage() {
  const { fileId = '', workspaceId = '' } = useParams({ strict: false });
  const { activeWorkspaceId, orgUuid, workspaces } = useWorkspace();
  const { msg } = useI18n();
  const formatters = useFormatters();
  const navigate = useNavigate();
  const [downloading, setDownloading] = useState(false);
  const [error, setError] = useState('');
  const workspaceExists = workspaces.some((workspace) => workspace.id === workspaceId);
  const scopeReady = workspaceExists && activeWorkspaceId === workspaceId;
  const query = useQuery({
    queryKey: ['file', orgUuid, workspaceId, fileId],
    queryFn: () => anthropicBetaApi.files.retrieveMetadata<ConsoleFile>(fileId, workspaceId),
    retry: false,
    enabled: scopeReady,
  });
  const file = scopeReady ? query.data : undefined;
  const download = async () => {
    if (!file || downloading) return;
    setDownloading(true);
    setError('');
    try {
      await downloadFile(file, workspaceId);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setDownloading(false);
    }
  };
  return (
    <section className="space-y-6">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => void navigate({ href: `/workspaces/${encodeURIComponent(workspaceId)}/files` })}
      >
        <ArrowLeft className="size-4" />
        {msg('files.title', 'Files')}
      </Button>
      <ResourcePageHeader
        title={file?.filename || fileId}
        actions={
          file && (
            <Button disabled={!file.downloadable || downloading} onClick={() => void download()}>
              <Download className="size-4" />
              {msg('common.download', 'Download')}
            </Button>
          )
        }
      />
      {workspaceExists && query.isPending && <p role="status">{msg('files.loading', 'Loading files...')}</p>}
      {!workspaceExists && <p role="alert">{msg('common.accessDenied', 'Access denied')}</p>}
      {query.isError && (
        <div role="alert">
          <p>{errorMessage(query.error)}</p>
          <Button variant="outline" onClick={() => void query.refetch()}>
            {msg('common.retry', 'Retry')}
          </Button>
        </div>
      )}
      {file && (
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-4 text-sm">
          <dt className="text-muted-foreground">ID</dt>
          <dd className="break-all font-mono text-xs">{file.id}</dd>
          <dt className="text-muted-foreground">{msg('common.size', 'Size')}</dt>
          <dd>{formatBytes(file.size_bytes)}</dd>
          <dt className="text-muted-foreground">{msg('common.created', 'Created')}</dt>
          <dd>{formatters.date(file.created_at, { dateStyle: 'medium', timeStyle: 'short' })}</dd>
        </dl>
      )}
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
    </section>
  );
}
