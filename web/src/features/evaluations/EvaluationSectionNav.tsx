import { useI18n } from '../../shared/i18n';
import {
  evaluationEvaluatorsHref,
  evaluationExperimentsHref,
  evaluationsIndexHref,
  useEvaluationWorkspace,
} from './useEvaluationDatasets';

export function EvaluationSectionNav({ current }: { current: 'datasets' | 'evaluators' | 'experiments' }) {
  const { msg } = useI18n();
  const workspace = useEvaluationWorkspace();
  const items = [
    {
      id: 'datasets' as const,
      href: evaluationsIndexHref(workspace.workspaceId),
      label: msg('evaluations.title', 'Datasets'),
    },
    {
      id: 'evaluators' as const,
      href: evaluationEvaluatorsHref(workspace.workspaceId),
      label: msg('evaluations.evaluators', 'Evaluators'),
    },
    {
      id: 'experiments' as const,
      href: evaluationExperimentsHref(workspace.workspaceId),
      label: msg('evaluations.experiments', 'Experiments'),
    },
  ];
  return (
    <nav className="mb-5 inline-flex rounded-lg bg-muted/80 p-0.5">
      {items.map((item) => {
        const selected = item.id === current;
        return (
          <a
            key={item.id}
            href={item.href}
            aria-current={selected ? 'page' : undefined}
            className={
              selected
                ? 'rounded-md bg-background px-3 py-1 text-sm font-medium text-foreground'
                : 'rounded-md px-3 py-1 text-sm text-muted-foreground hover:text-foreground'
            }
          >
            {item.label}
          </a>
        );
      })}
    </nav>
  );
}
