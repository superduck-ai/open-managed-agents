import { useI18n } from '../../shared/i18n';
import { Checkbox } from '../../shared/ui/checkbox';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';

export function SessionCaseFields({
  idPrefix,
  name = '',
  desc,
  checkpoints,
  checkpointError,
  showName = true,
  onName,
  onDesc,
  onCheckpoints,
}: {
  idPrefix: string;
  name?: string;
  desc: string;
  checkpoints: string;
  checkpointError: boolean;
  showName?: boolean;
  onName?: (value: string) => void;
  onDesc: (value: string) => void;
  onCheckpoints: (value: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      {showName ? (
        <>
          <Label htmlFor={`${idPrefix}-name`}>{msg('evaluations.name', 'Name')}</Label>
          <Input id={`${idPrefix}-name`} value={name} onChange={(event) => onName?.(event.target.value)} />
        </>
      ) : null}
      <Label htmlFor={`${idPrefix}-description`}>{msg('evaluations.descriptionField', 'Description')}</Label>
      <Input id={`${idPrefix}-description`} value={desc} onChange={(event) => onDesc(event.target.value)} />
      <CheckpointsField
        id={`${idPrefix}-checkpoints`}
        value={checkpoints}
        onChange={onCheckpoints}
        error={checkpointError}
      />
    </>
  );
}

export function CheckpointsField({
  id,
  value,
  onChange,
  error = false,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
  error?: boolean;
}) {
  const { msg } = useI18n();
  return (
    <>
      <Label htmlFor={id}>{msg('evaluations.checkpoints', 'Checkpoints')}</Label>
      <Textarea
        id={id}
        className="placeholder:whitespace-pre-line"
        value={value}
        placeholder={msg(
          'evaluations.checkpointsPlaceholder',
          'The task finishes without an error\nThe final reply includes the conclusion\nProduce a PDF report',
        )}
        onChange={(event) => onChange(event.target.value)}
      />
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.checkpointsHint',
          'An acceptance checklist for evaluators, not instructions sent to the agent under test. One checkpoint per line, or paste a JSON array.',
        )}
      </p>
      {error ? (
        <p className="text-sm text-destructive">
          {msg(
            'evaluations.checkpointsInvalid',
            'Write one checkpoint per line, or paste a string array such as ["The task finishes without an error"].',
          )}
        </p>
      ) : null}
    </>
  );
}

export function IncludeOutputFilesField({
  id,
  checked,
  onChange,
}: {
  id: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  const { msg } = useI18n();
  return (
    <div className="grid gap-1">
      <Label htmlFor={id} className="items-start gap-2">
        <Checkbox id={id} checked={checked} onCheckedChange={(next) => onChange(next === true)} />
        <span>{msg('evaluations.includeOutputFiles', 'Include output files as reference files')}</span>
      </Label>
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.includeOutputFilesHint',
          'Output files stay out unless you opt in. They are often the behavior under test.',
        )}
      </p>
    </div>
  );
}
