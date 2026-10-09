import { ArrowUpRight, KeyRound } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { workspaceApiKeysPath } from '../../../shared/workspaces/presentation';
import { ButtonLink } from '../../../shared/ui/button';
import { quickstartCopy } from './copy';

export function QuickstartApiKey({ workspaceID }: { workspaceID: string }) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  return (
    <div className="space-y-2">
      <h2 className="font-semibold">{text.key}</h2>
      <p className="text-sm leading-relaxed text-muted-foreground">{text.keyHint}</p>
      <ButtonLink
        href={workspaceApiKeysPath(workspaceID)}
        target="_blank"
        rel="noopener noreferrer"
        variant="outline"
        className="h-auto min-h-9 bg-background py-2 text-sm whitespace-normal"
      >
        <KeyRound />
        {text.keyLink}
        <ArrowUpRight />
      </ButtonLink>
    </div>
  );
}
