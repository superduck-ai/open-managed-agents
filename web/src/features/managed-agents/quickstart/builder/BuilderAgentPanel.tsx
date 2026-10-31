import { useState } from 'react';
import { parse, stringify } from 'yaml';
import { diffLines } from 'diff';
import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
import { ToggleGroup, ToggleGroupItem } from '../../../../shared/ui/toggle-group';
import { AgentConfigEditor } from '../../components/AgentConfigEditor';
import { createAgentDraftSchema } from '../../agents/create-dialog-model';
import { ManagedErrorAlert } from '../../components/common';
import type { Builder } from './useBuilder';
import type { CodeFormat } from '../../types';
import type { BuilderState } from './model';

export function BuilderAgentPanel({ builder }: { builder: Builder }) {
  const { locale } = useI18n();
  const labels = panelLabels(locale === 'zh-CN');
  const { state } = builder;
  const proposal = state.gate?.name === 'build_agent_config';
  const value = proposal ? state.config : (state.savedConfig ?? state.config);
  const [format, setFormat] = useState<CodeFormat>('YAML');
  const [view, setView] = useState(proposal && state.savedConfig ? 'changes' : 'config');
  const [draft, setDraft] = useState('');
  const serialized = serializeConfig(value, format);
  const text = builder.draftEditing ? draft : serialized;
  const editable = !state.agent && proposal && !builder.busy && !state.operation;
  const read = (input: string) => createAgentDraftSchema.parse(format === 'YAML' ? parse(input) : JSON.parse(input));
  let validation = '';
  try {
    read(text);
  } catch (cause) {
    validation = cause instanceof Error ? cause.message : String(cause);
  }
  const changeFormat = (next: string | null) => {
    if (!next || validation) return;
    if (builder.draftEditing) setDraft(next === 'YAML' ? stringify(read(text)) : JSON.stringify(read(text), null, 2));
    setFormat(next as CodeFormat);
  };
  return (
    <>
      <div className="flex shrink-0 flex-wrap items-baseline gap-x-3 gap-y-1">
        <p className="break-words font-medium">{value.name}</p>
        <p className="text-sm text-muted-foreground">{configStatus(state, proposal, locale === 'zh-CN')}</p>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        {proposal && state.savedConfig && (
          <ToggleGroup
            value={[view]}
            onValueChange={(values) => {
              if (values[0]) setView(values[0]);
            }}
            aria-label={labels.view}
          >
            <ToggleGroupItem value="changes">{labels.changes}</ToggleGroupItem>
            <ToggleGroupItem value="config">{labels.config}</ToggleGroupItem>
          </ToggleGroup>
        )}
        <ToggleGroup
          value={[format]}
          onValueChange={(values) => changeFormat(values[0])}
          aria-label={labels.format}
          disabled={Boolean(validation)}
        >
          <ToggleGroupItem value="YAML">YAML</ToggleGroupItem>
          <ToggleGroupItem value="JSON">JSON</ToggleGroupItem>
        </ToggleGroup>
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {view === 'changes' && proposal && state.savedConfig ? (
          <ConfigChanges
            original={serializeConfig(state.savedConfig, format)}
            proposed={serialized}
            label={labels.changes}
          />
        ) : (
          <AgentConfigEditor
            value={text}
            format={format}
            onChange={(next) => {
              setDraft(next);
              builder.setDraftEditing(true);
            }}
            lineNumbers
            minHeight="0"
            readOnly={!editable}
            ariaLabel={labels.content}
          />
        )}
      </div>
      {validation && <ManagedErrorAlert>{validation}</ManagedErrorAlert>}

      {builder.draftEditing && (
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Button
            disabled={Boolean(validation) || builder.busy}
            onClick={async () => {
              if (await builder.editDraft(read(text))) setDraft('');
            }}
          >
            {labels.apply}
          </Button>
          <Button
            variant="outline"
            disabled={builder.busy}
            onClick={() => {
              setDraft('');
              builder.setDraftEditing(false);
            }}
          >
            {labels.discard}
          </Button>
          <p className="text-xs text-muted-foreground">{labels.notice}</p>
        </div>
      )}
    </>
  );
}

function configStatus(state: BuilderState, proposal: boolean, zh: boolean) {
  if (!state.agent) return zh ? '未保存草稿' : 'Unsaved draft';
  if (!proposal) return `${zh ? '已保存' : 'Saved'} v${state.agent.version}`;
  return `${zh ? '待确认' : 'Proposed'}: v${state.agent.version} → v${state.agent.version + 1}`;
}
function panelLabels(zh: boolean) {
  return zh
    ? {
        view: '配置视图',
        changes: '变更对比',
        config: '完整配置',
        format: '配置格式',
        content: '智能体配置内容',
        apply: '应用草稿',
        discard: '放弃编辑',
        notice: '有未应用修改',
      }
    : {
        view: 'Configuration view',
        changes: 'Changes',
        config: 'Configuration',
        format: 'Configuration format',
        content: 'Agent configuration content',
        apply: 'Apply draft',
        discard: 'Discard edits',
        notice: 'Unapplied changes',
      };
}
function ConfigChanges({ original, proposed, label }: { original: string; proposed: string; label: string }) {
  return (
    <div role="region" className="overflow-auto p-3 font-mono text-xs leading-6" aria-label={label}>
      {diffLines(original, proposed).map((part, index) => (
        <pre
          key={index}
          className={`whitespace-pre-wrap break-words ${part.added ? 'bg-[var(--success-bg)] text-success' : part.removed ? 'bg-[var(--red-a3)] text-destructive' : 'text-muted-foreground'}`}
        >
          {part.value
            .split('\n')
            .filter((line, i, lines) => i < lines.length - 1 || line)
            .map((line) => `${part.added ? '+' : part.removed ? '−' : ' '} ${line}`)
            .join('\n')}
        </pre>
      ))}
    </div>
  );
}

function serializeConfig(value: unknown, format: CodeFormat) {
  return format === 'YAML' ? stringify(value) : JSON.stringify(value, null, 2);
}
