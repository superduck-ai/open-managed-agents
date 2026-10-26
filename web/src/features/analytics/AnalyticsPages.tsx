import { ExternalLink } from 'lucide-react';
export { UsagePage, CostPage } from './MeteredAnalyticsPage';
import type { ReactNode } from 'react';
import { useI18n } from '../../shared/i18n';
import { ButtonLink } from '../../shared/ui/button';
import { Empty, EmptyContent, EmptyHeader, EmptyTitle } from '../../shared/ui/empty';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';

export function CachingPage() {
  const { msg } = useI18n();
  return (
    <AnalyticsPageRoot
      title={msg('analytics.caching.title', 'Caching')}
      notice={msg('analytics.caching.unavailable', 'Caching data is not connected.')}
    >
      <ButtonLink
        href="https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching"
        target="_blank"
        rel="noreferrer"
        variant="outline"
      >
        {msg('common.learnMore', 'Learn more')}
        <ExternalLink className="size-4" aria-hidden />
      </ButtonLink>
    </AnalyticsPageRoot>
  );
}

export function RateLimitsPage() {
  const { msg } = useI18n();
  return (
    <AnalyticsPageRoot
      title={msg('analytics.rateLimits.title', 'Rate limits')}
      notice={msg('analytics.rateLimits.unavailable', 'Rate limit data is not connected.')}
    />
  );
}

export function LogsPage() {
  const { msg } = useI18n();
  return (
    <AnalyticsPageRoot
      title={msg('analytics.logs.title', 'Logs')}
      notice={msg('analytics.logs.unavailable', 'Request logs are not connected.')}
    />
  );
}

function AnalyticsPageRoot({ title, notice, children }: { title: string; notice: string; children?: ReactNode }) {
  return (
    <section data-testid="analytics-page">
      <ResourcePageHeader title={title} contentGap="content" />
      <Empty className="min-h-[320px] border-0">
        <EmptyHeader>
          <EmptyTitle className="text-sm font-medium text-muted-foreground">{notice}</EmptyTitle>
        </EmptyHeader>
        {children ? <EmptyContent>{children}</EmptyContent> : null}
      </Empty>
    </section>
  );
}
