import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '../../../shared/ui/collapsible';
import { type ReactNode, useState } from 'react';
import { analyzeAlertText } from './alertText';

export function AlertMessage({ children }: { children: ReactNode }) {
  if (typeof children !== 'string') {
    return <>{children}</>;
  }
  return <AlertMessageContent key={children} text={children} />;
}

function AlertMessageContent({ text }: { text: string }) {
  const { msg } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const analysis = analyzeAlertText(text);
  if (!analysis) {
    return null;
  }
  const summary = analysis.html
    ? analysis.status === null
      ? msg('common.errorUnexpectedResponse', 'Request failed: the server returned an unrecognized response.')
      : msg('common.errorHttpPage', 'Request failed: the server returned an HTTP {status} page.', {
          status: analysis.status,
        })
    : analysis.summary;
  const detailLabel = analysis.html
    ? expanded
      ? msg('common.hideRawError', 'Hide raw error')
      : msg('common.showRawError', 'Show raw error')
    : expanded
      ? msg('common.hideDetails', 'Hide details')
      : msg('common.showDetails', 'Show details');
  return (
    <div className="grid min-w-0 gap-1.5">
      <div className="min-w-0 whitespace-pre-wrap break-words">{summary}</div>
      {analysis.detail ? (
        <Collapsible open={expanded} onOpenChange={setExpanded}>
          <CollapsibleTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                size="xs"
                className="-ml-1.5 h-6 w-fit px-1.5 text-xs font-medium text-inherit hover:bg-current/10"
              />
            }
          >
            {detailLabel}
          </CollapsibleTrigger>
          <CollapsibleContent>
            <pre className="mt-1 max-h-40 overflow-auto rounded-md border border-current/15 bg-background/40 p-2 font-mono text-[11px] leading-5 break-all whitespace-pre-wrap">
              {analysis.detail}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      ) : null}
    </div>
  );
}
