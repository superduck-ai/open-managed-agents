import { useI18n } from '../../../shared/i18n';
import { ButtonLink } from '../../../shared/ui/button';
import { handleInternalLinkClick } from '../utils';

export function ResourceNotFound({
  title,
  sentence,
  backHref,
  backLabel,
}: {
  title: string;
  sentence: string;
  backHref: string;
  backLabel: string;
}) {
  return (
    <section className="min-h-[calc(100vh-48px)] text-foreground">
      <h1 className="text-[28px] font-semibold leading-tight text-foreground">{title}</h1>
      <p className="mt-2 max-w-[760px] text-[15px] leading-5 text-muted-foreground">{sentence}</p>
      <ButtonLink
        href={backHref}
        variant="outline"
        size="lg"
        className="mt-6"
        onClick={(event) => handleInternalLinkClick(event, backHref)}
      >
        {backLabel}
      </ButtonLink>
    </section>
  );
}

export function resourceMissingCopy(
  loadError: string | null,
  copy: { title: string; sentence: string; failureTitle: string },
) {
  const notFound = !loadError || /not found/i.test(loadError);
  return {
    title: notFound ? copy.title : copy.failureTitle,
    sentence: notFound ? copy.sentence : loadError || copy.sentence,
  };
}

export function useResourceMissingCopy(loadError: string | null, kind: 'agent' | 'session', id: string) {
  const { msg } = useI18n();
  if (kind === 'agent') {
    return resourceMissingCopy(loadError, {
      title: msg('managedAgents.agents.notFoundTitle', 'Agent not found'),
      sentence: msg('managedAgents.agents.notFoundSentence', 'Agent {id} was not found.', { id }),
      failureTitle: msg('managedAgents.agents.detail.loadFailed', 'Could not load agent'),
    });
  }
  return resourceMissingCopy(loadError, {
    title: msg('managedAgents.sessions.detail.notFoundTitle', 'Session not found'),
    sentence: msg('managedAgents.sessions.detail.notFoundSentence', 'Session {id} was not found.', { id }),
    failureTitle: msg('managedAgents.sessions.detail.loadFailed', 'Could not load session'),
  });
}
