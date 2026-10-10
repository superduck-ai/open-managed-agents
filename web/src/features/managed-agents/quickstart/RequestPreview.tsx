import { useState } from 'react';
import { Check, Copy } from 'lucide-react';
import { anthropicBaseURL } from '../../../shared/api/anthropic';
import { Button } from '../../../shared/ui/button';
import { SyntaxCodeBlock } from '../components/CodeBlocks';
import { quickstartCopy } from './copy';
import { useI18n } from '../../../shared/i18n';

function shellQuote(value: string) {
  return `'${value.replace(/'/g, `'"'"'`)}'`;
}

export function quickstartCurl(path: string, workspaceID: string, body?: unknown, stream = false) {
  const url = new URL(`${anthropicBaseURL()}${path}`);
  url.searchParams.set('beta', 'true');
  const lines = [
    `curl ${stream ? '-N ' : ''}${body ? '-X POST ' : ''}${shellQuote(url.href)}`,
    `  -H 'x-api-key: YOUR_API_KEY'`,
    `  -H 'anthropic-version: 2023-06-01'`,
    `  -H ${shellQuote(`x-workspace-id: ${workspaceID}`)}`,
  ];
  if (stream) lines.push(`  -H 'Accept: text/event-stream'`);
  if (body) lines.push(`  -H 'Content-Type: application/json'`, `  -d ${shellQuote(JSON.stringify(body, null, 2))}`);
  return lines.join(' \\\n');
}

export function RequestPreview({ code, hint = true }: { code: string; hint?: boolean }) {
  const { locale } = useI18n();
  const text = quickstartCopy(locale);
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle');
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code);
      setCopyState('copied');
    } catch {
      setCopyState('failed');
    }
  };
  return (
    <div className="min-w-0 space-y-3">
      <div className="flex items-center justify-between gap-3">
        <span className="text-xs font-medium text-muted-foreground">curl</span>
        <Button type="button" variant="ghost" size="sm" onClick={() => void copy()}>
          <span>{copyState === 'copied' ? text.copied : text.copy}</span>
          {copyState === 'copied' ? <Check /> : <Copy />}
        </Button>
      </div>
      <SyntaxCodeBlock value={code} language="bash" />
      {copyState === 'failed' && (
        <p role="status" className="text-sm text-muted-foreground">
          {text.copyFailed}
        </p>
      )}
      {hint && <p className="text-xs leading-relaxed text-muted-foreground">{text.apiHint}</p>}
    </div>
  );
}
