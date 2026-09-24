import { useState } from 'react';
import { LoaderCircle } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { DialogContent, DialogHeader, DialogTitle, DialogDescription } from '../../../shared/ui/dialog';
import { prebuildStepState, type PrebuildStage, type EnvironmentPrebuild } from './prebuild';
import { PrebuildIcon, PrebuildLabel, PrebuildTime, PrebuildDuration, PrebuildStateText } from './prebuild-display';
import { EnvironmentPrebuildLogs } from './prebuild-logs';

type PrebuildActions = {
  prebuild: EnvironmentPrebuild;
  readOnly?: boolean;
  busy: boolean;
  error: boolean;
  onAction: (action: 'start' | 'cancel') => void;
};

export function EnvironmentPrebuildDialog({
  environmentId,
  workspaceId,
  ...actions
}: PrebuildActions & {
  environmentId: string;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const { prebuild } = actions;
  return (
    <DialogContent className="environment-prebuild-panel flex h-[min(620px,85dvh)] flex-col gap-0 overflow-hidden p-0 text-foreground sm:max-w-4xl">
      <DialogHeader className="shrink-0 px-5 pt-5 pb-3 pr-12">
        <DialogTitle>{msg('environmentPrebuild.title', 'Package preparation')}</DialogTitle>
        <DialogDescription className="sr-only">
          {msg('environmentPrebuild.dialogDescription', 'Preparation status and logs for the saved packages.')}
        </DialogDescription>
      </DialogHeader>
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-border px-5 py-3">
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
          <PrebuildLabel state={prebuild.state} />
          <span className="text-muted-foreground">
            <PrebuildTime prebuild={prebuild} />
          </span>
          {prebuild.createdAt ? (
            <span className="text-muted-foreground">
              <PrebuildDuration prebuild={prebuild} />
            </span>
          ) : null}
        </div>
        <PrebuildAction {...actions} />
      </div>
      <PrebuildNotice {...actions} />
      <PrebuildRun
        key={prebuild.jobId || 'current'}
        prebuild={prebuild}
        workspaceId={workspaceId}
        environmentId={environmentId}
      />
    </DialogContent>
  );
}

function PrebuildAction({ prebuild, readOnly, busy, onAction }: PrebuildActions) {
  const { msg } = useI18n();
  if (readOnly || (!prebuild.canStart && !prebuild.canCancel)) return null;
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      disabled={busy}
      onClick={() => onAction(prebuild.canCancel ? 'cancel' : 'start')}
    >
      {busy ? <LoaderCircle aria-hidden className="animate-spin motion-reduce:animate-none" /> : null}
      {prebuild.canCancel
        ? msg('common.cancel', 'Cancel')
        : prebuild.state === 'idle'
          ? msg('environmentPrebuild.start', 'Prepare now')
          : msg('common.retry', 'Retry')}
    </Button>
  );
}

function PrebuildNotice({ prebuild, error }: PrebuildActions) {
  const { msg } = useI18n();
  const notice =
    prebuild.state === 'unknown'
      ? msg('environmentPrebuild.uncertain', 'The previous result is unconfirmed. Retrying may run it again.')
      : null;
  return (
    <>
      {notice ? <p className="px-5 pb-3 text-xs text-muted-foreground">{notice}</p> : null}
      {error ? (
        <p role="alert" className="px-5 pb-3 text-xs text-destructive">
          {msg('environmentPrebuild.actionFailed', 'Could not complete the request. Try again.')}
        </p>
      ) : null}
    </>
  );
}

function PrebuildRun({
  prebuild,
  environmentId,
  workspaceId,
}: {
  prebuild: EnvironmentPrebuild;
  environmentId: string;
  workspaceId: string;
}) {
  const { msg } = useI18n();
  const [selectedStage, setStage] = useState<PrebuildStage | null>(null);
  const stage = selectedStage ?? (prebuild.stage || 'image');
  const hasLogs = stage === 'image' ? prebuild.hasImageLogs : prebuild.hasTemplateLogs;
  const failure =
    stage === prebuild.stage && (prebuild.state === 'failed' || prebuild.state === 'unknown') && prebuild.message;
  const stageLabel = (value: PrebuildStage) =>
    value === 'image'
      ? msg('environmentPrebuild.image', 'Build image')
      : msg('environmentPrebuild.template', 'Build template');
  return (
    <div className="flex min-h-0 flex-1 flex-col border-t border-border sm:flex-row">
      <div
        aria-label={msg('environmentPrebuild.stages', 'Build stages')}
        className="flex shrink-0 gap-1 border-b border-border p-2 sm:w-44 sm:flex-col sm:border-r sm:border-b-0"
      >
        {(['image', 'template'] as const).map((value) => (
          <Button
            key={value}
            type="button"
            variant="ghost"
            size="sm"
            aria-pressed={stage === value}
            className={`min-w-0 flex-1 justify-start sm:flex-none ${stage === value ? 'bg-muted' : 'text-muted-foreground'}`}
            onClick={() => setStage(value)}
          >
            <PrebuildIcon state={prebuildStepState(prebuild, value)} />
            {stageLabel(value)}
            <span className="sr-only">
              <PrebuildStateText state={prebuildStepState(prebuild, value)} />
            </span>
          </Button>
        ))}
      </div>
      <section aria-label={stageLabel(stage)} className="flex min-h-0 min-w-0 flex-1 flex-col">
        {hasLogs && prebuild.jobId ? (
          <EnvironmentPrebuildLogs
            key={stage}
            environmentId={environmentId}
            workspaceId={workspaceId}
            jobId={prebuild.jobId}
            stage={stage}
          />
        ) : failure ? (
          <pre className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-words bg-muted/30 p-4 font-mono text-xs leading-relaxed">
            {failure}
          </pre>
        ) : (
          <div className="flex min-h-40 flex-1 items-center justify-center p-6 text-center text-sm text-muted-foreground">
            {!prebuild.jobId && prebuild.state !== 'idle'
              ? msg('environmentPrebuild.expired', 'Run details are no longer available.')
              : stage === 'template' && prebuild.stage === 'template'
                ? msg('environmentPrebuild.logsUnsupported', 'Logs are unavailable for this stage.')
                : msg('environmentPrebuild.noLogs', 'No logs yet.')}
          </div>
        )}
      </section>
    </div>
  );
}
