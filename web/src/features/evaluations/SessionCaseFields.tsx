import { useI18n } from '../../shared/i18n';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';

export function SessionCaseFields({
  idPrefix,
  name,
  desc,
  checkpoints,
  checkpointError,
  onName,
  onDesc,
  onCheckpoints,
}: {
  idPrefix: string;
  name: string;
  desc: string;
  checkpoints: string;
  checkpointError: boolean;
  onName: (value: string) => void;
  onDesc: (value: string) => void;
  onCheckpoints: (value: string) => void;
}) {
  const { msg } = useI18n();
  return (
    <>
      <Label htmlFor={`${idPrefix}-name`}>{msg('evaluations.name', 'Name')}</Label>
      <Input id={`${idPrefix}-name`} value={name} onChange={(event) => onName(event.target.value)} />
      <Label htmlFor={`${idPrefix}-description`}>{msg('evaluations.descriptionField', 'Description')}</Label>
      <Input id={`${idPrefix}-description`} value={desc} onChange={(event) => onDesc(event.target.value)} />
      <Label htmlFor={`${idPrefix}-checkpoints`}>{msg('evaluations.checkpoints', 'Checkpoints')}</Label>
      <Textarea
        id={`${idPrefix}-checkpoints`}
        value={checkpoints}
        onChange={(event) => onCheckpoints(event.target.value)}
      />
      <p className="text-sm text-muted-foreground">
        {msg('evaluations.checkpointsHint', 'Optional JSON array or object.')}
      </p>
      {checkpointError ? (
        <p className="text-sm text-destructive">
          {msg('evaluations.checkpointsInvalid', 'Checkpoints must be a JSON array or object.')}
        </p>
      ) : null}
    </>
  );
}
