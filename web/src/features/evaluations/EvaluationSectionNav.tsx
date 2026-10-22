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
    <nav className="mb-4 flex gap-4 text-sm">
      {items.map((item) => (
        <a
          key={item.id}
          href={item.href}
          aria-current={item.id === current ? 'page' : undefined}
          className={
            item.id === current ? 'font-medium text-foreground' : 'text-muted-foreground hover:text-foreground'
          }
        >
          {item.label}
        </a>
      ))}
    </nav>
  );
}
