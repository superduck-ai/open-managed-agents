import { useState, type ChangeEvent, type FormEvent } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Label } from '../../shared/ui/label';
import { Textarea } from '../../shared/ui/textarea';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { evaluationErrorMessage, type EvaluationExperiment, type EvaluationExperimentScore } from './api';
import { EvaluationSectionNav } from './EvaluationSectionNav';
import { useCompareEvaluationExperiments } from './useEvaluationExperiments';

export function ExperimentComparePage() {
  const { msg } = useI18n();
  const compare = useCompareEvaluationExperiments();
  const [submitted, setSubmitted] = useState(false);
  const [datasetVersionId, setDatasetVersionId] = useState('');
  const [evaluatorIds, setEvaluatorIds] = useState('');
  const experiments = compare.data?.data ?? [];

  function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitted(true);
    compare.mutate({
      dataset_version_id: datasetVersionId,
      evaluator_ids: evaluatorIds
        .split(/[\s,]+/)
        .map((item) => item.trim())
        .filter((item) => item.length > 0),
    });
  }

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <EvaluationSectionNav current="experiments" />
      <header className="mb-5">
        <h1 className="text-2xl font-semibold tracking-tight">{msg('evaluations.compare', 'Compare')}</h1>
      </header>
      <form className="mb-6 grid max-w-xl gap-4" onSubmit={onSubmit}>
        <div className="grid gap-2">
          <Label htmlFor="dataset_version_id">{msg('evaluations.datasetVersion', 'Dataset version')}</Label>
          <Input
            id="dataset_version_id"
            value={datasetVersionId}
            onChange={(event: ChangeEvent<HTMLInputElement>) => setDatasetVersionId(event.target.value)}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="evaluator_ids">{msg('evaluations.evaluatorIds', 'Evaluators')}</Label>
          <Textarea
            id="evaluator_ids"
            value={evaluatorIds}
            onChange={(event: ChangeEvent<HTMLTextAreaElement>) => setEvaluatorIds(event.target.value)}
          />
        </div>
        {compare.error ? <p className="text-sm text-destructive">{evaluationErrorMessage(compare.error)}</p> : null}
        <Button type="submit" className="h-9 w-fit" disabled={compare.isPending}>
          {msg('evaluations.compareAction', 'Compare')}
        </Button>
      </form>
      <CompareResults submitted={submitted && !compare.isPending} experiments={experiments} />
    </section>
  );
}

function CompareResults({ submitted, experiments }: { submitted: boolean; experiments: EvaluationExperiment[] }) {
  const { msg } = useI18n();
  if (!submitted) {
    return null;
  }
  if (experiments.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">{msg('evaluations.compareEmpty', 'No completed experiments.')}</p>
    );
  }
  const evaluatorIDs = compareEvaluatorIDs(experiments);
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{msg('evaluations.name', 'Name')}</TableHead>
          <TableHead>{msg('evaluations.agentId', 'Agent')}</TableHead>
          <TableHead>{msg('evaluations.agentVersion', 'Agent version')}</TableHead>
          <TableHead>{msg('evaluations.status', 'Status')}</TableHead>
          {evaluatorIDs.map((evaluatorID) => (
            <TableHead key={evaluatorID}>{evaluatorID}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {experiments.map((experiment) => (
          <TableRow key={experiment.id}>
            <TableCell>{experiment.name}</TableCell>
            <TableCell>{experiment.agent_id}</TableCell>
            <TableCell>{experiment.agent_version}</TableCell>
            <TableCell>{experiment.status}</TableCell>
            {evaluatorIDs.map((evaluatorID) => (
              <TableCell key={evaluatorID}>{compareScoreText(experiment, evaluatorID, msg)}</TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

type CompareMessage = (key: string, fallback: string) => string;

function compareEvaluatorIDs(experiments: EvaluationExperiment[]) {
  const ordered: string[] = [];
  const seen = new Set<string>();
  const evaluators = experiments.flatMap((experiment) => experiment.evaluators ?? []);
  evaluators.sort((left, right) => left.position - right.position);
  for (const evaluator of evaluators) {
    if (seen.has(evaluator.id)) {
      continue;
    }
    seen.add(evaluator.id);
    ordered.push(evaluator.id);
  }
  for (const experiment of experiments) {
    for (const score of experiment.scores ?? []) {
      if (seen.has(score.evaluator_id)) {
        continue;
      }
      seen.add(score.evaluator_id);
      ordered.push(score.evaluator_id);
    }
  }
  return ordered;
}

function compareScoreText(experiment: EvaluationExperiment, evaluatorID: string, msg: CompareMessage) {
  const scores = (experiment.scores ?? []).filter((score) => score.evaluator_id === evaluatorID);
  if (scores.length === 0) {
    return msg('evaluations.scoreMissing', 'Missing');
  }
  return scores.map((score) => compareScoreValue(score, msg)).join(', ');
}

function compareScoreValue(score: EvaluationExperimentScore, msg: CompareMessage) {
  if (score.status === 'failed') {
    return score.error || msg('evaluations.scoreFailed', 'Failed');
  }
  if (score.status === 'completed' && typeof score.score === 'number') {
    return String(score.score);
  }
  return msg('evaluations.scoreMissing', 'Missing');
}
