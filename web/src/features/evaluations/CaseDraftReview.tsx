import { useI18n } from '../../shared/i18n';
import { Badge } from '../../shared/ui/badge';
import { Checkbox } from '../../shared/ui/checkbox';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';
import { toolTraceText, type GenerationStats, type ReviewDraft } from './caseGeneration';

export function CaseDraftReview({
  drafts,
  stats,
  onChange,
  disabled,
}: {
  drafts: ReviewDraft[];
  stats: GenerationStats;
  onChange: (drafts: ReviewDraft[]) => void;
  disabled: boolean;
}) {
  const { msg } = useI18n();
  const selectedCount = drafts.filter((draft) => draft.selected).length;

  function update(key: string, patch: Partial<ReviewDraft>) {
    onChange(drafts.map((draft) => (draft.key === key ? { ...draft, ...patch } : draft)));
  }

  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <p className="text-sm font-medium">{msg('evaluations.reviewTitle', 'Review generated cases')}</p>
        <p className="text-sm text-muted-foreground">
          {msg(
            'evaluations.reviewStats',
            'Asked for {requested}. Removed {duplicates} duplicates. {failed} could not be generated.',
            stats,
          )}
        </p>
      </div>
      {drafts.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {msg('evaluations.reviewEmpty', 'No usable cases. Go back and adjust the plan.')}
        </p>
      ) : (
        <Label className="flex items-center gap-2">
          <Checkbox
            checked={selectedCount === drafts.length}
            indeterminate={selectedCount > 0 && selectedCount < drafts.length}
            disabled={disabled}
            onCheckedChange={(checked) => onChange(drafts.map((draft) => ({ ...draft, selected: checked })))}
          />
          {msg('evaluations.reviewSelected', '{selected} of {total} selected', {
            selected: selectedCount,
            total: drafts.length,
          })}
        </Label>
      )}
      {drafts.map((draft) => (
        <DraftCard key={draft.key} draft={draft} disabled={disabled} onChange={(patch) => update(draft.key, patch)} />
      ))}
    </div>
  );
}

function DraftCard({
  draft,
  disabled,
  onChange,
}: {
  draft: ReviewDraft;
  disabled: boolean;
  onChange: (patch: Partial<ReviewDraft>) => void;
}) {
  const { msg } = useI18n();
  const tools = toolTraceText(draft.expected_tool_trace);
  return (
    <div className={`grid gap-3 rounded-xl border border-border p-3 ${draft.selected ? '' : 'opacity-60'}`}>
      <div className="flex flex-wrap items-center gap-2">
        <Checkbox
          aria-label={msg('evaluations.keepDraft', 'Keep this case')}
          checked={draft.selected}
          disabled={disabled}
          onCheckedChange={(checked) => onChange({ selected: checked })}
        />
        <Badge variant="outline">{draft.scenario}</Badge>
        <Badge variant={draft.polarity === 'negative' ? 'destructive' : 'secondary'}>
          {draft.polarity === 'negative'
            ? msg('evaluations.negative', 'Negative')
            : msg('evaluations.positive', 'Positive')}
        </Badge>
      </div>
      <Textarea
        aria-label={msg('evaluations.query', 'Query')}
        value={draft.query}
        rows={2}
        disabled={disabled}
        onChange={(event) => onChange({ query: event.target.value })}
      />
      <Textarea
        aria-label={msg('evaluations.expectedAnswer', 'Expected answer')}
        value={draft.expected_answer}
        rows={2}
        disabled={disabled}
        placeholder={msg('evaluations.unsetAnswer', 'No expected answer')}
        onChange={(event) => onChange({ expected_answer: event.target.value })}
      />
      {draft.polarity === 'negative' ? (
        <Textarea
          aria-label={msg('evaluations.acceptance', 'What counts as fixed')}
          value={draft.acceptance}
          rows={3}
          disabled={disabled}
          placeholder={msg('evaluations.acceptanceLines', 'One check per line')}
          onChange={(event) => onChange({ acceptance: event.target.value })}
        />
      ) : null}
      {tools ? (
        <p className="text-xs text-muted-foreground">
          {msg('evaluations.expectedTools', 'Expected tools: {tools}', { tools })}
        </p>
      ) : null}
    </div>
  );
}
