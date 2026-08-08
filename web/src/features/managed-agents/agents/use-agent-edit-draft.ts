import { useCallback, useMemo, useState } from 'react';
import { agentEditConfigText, parseAgentEditConfigText } from '../agentConfig';
import { type AgentEditConfig, type CodeFormat, type CreateAgentInput } from '../types';
import { createAgentDraftSchema, normalizeCreateAgentDraft, type CreateAgentView } from './create-dialog-model';

type RenderedDraftResult =
  { ok: true; draft: CreateAgentInput; error: null } | { ok: false; draft: null; error: string };

export function renderedAgentEditDraft(config: AgentEditConfig): RenderedDraftResult {
  const candidate = {
    name: config.name,
    description: config.description ?? null,
    model: config.model,
    system: config.system ?? null,
    mcp_servers: config.mcp_servers ?? [],
    tools: config.tools ?? [],
    skills: config.skills ?? [],
    ...(config.metadata === undefined ? {} : { metadata: config.metadata }),
    ...(config.multiagent === undefined ? {} : { multiagent: config.multiagent }),
  };
  const parsed = createAgentDraftSchema.safeParse(candidate);
  if (!parsed.success) {
    return {
      ok: false,
      draft: null,
      error: 'This configuration contains fields that cannot be edited safely in Rendered mode. Continue in Raw.',
    };
  }
  return { ok: true, draft: normalizeCreateAgentDraft(parsed.data), error: null };
}

export function useAgentEditDraft(initialConfig: AgentEditConfig) {
  const initialRendered = renderedAgentEditDraft(initialConfig);
  const [draft, setDraftState] = useState<AgentEditConfig>(initialConfig);
  const [renderedDraft, setRenderedDraftState] = useState<CreateAgentInput | null>(initialRendered.draft);
  const [renderedError, setRenderedError] = useState<string | null>(initialRendered.error);
  const [view, setViewState] = useState<CreateAgentView>(initialRendered.ok ? 'rendered' : 'raw');
  const [format, setFormatState] = useState<CodeFormat>('YAML');
  const [rawText, setRawText] = useState(() => agentEditConfigText(initialConfig, 'YAML'));
  const [rawError, setRawError] = useState<string | null>(null);
  const renderedDraftError = useMemo(() => {
    if (!renderedDraft) {
      return null;
    }
    const parsed = createAgentDraftSchema.safeParse(renderedDraft);
    return parsed.success
      ? null
      : parsed.error.issues
          .slice(0, 3)
          .map((issue) => `${issue.path.join('.') || 'Agent configuration'}: ${issue.message}`)
          .join(' ');
  }, [renderedDraft]);

  const setRenderedDraft = useCallback((next: CreateAgentInput) => {
    setRenderedDraftState(next);
    setRenderedError(null);
    setDraftState(next);
  }, []);

  const selectView = useCallback(
    (nextView: CreateAgentView) => {
      if (nextView === view) {
        return true;
      }
      if (nextView === 'raw') {
        const nextRawText = agentEditConfigText(draft, format);
        const parsed = parseAgentEditConfigText(nextRawText, format);
        setRawText(nextRawText);
        setRawError(parsed.ok ? null : parsed.error);
        setViewState(nextView);
        return true;
      }
      if (rawError || renderedError || !renderedDraft) {
        return false;
      }
      setViewState(nextView);
      return true;
    },
    [draft, format, rawError, renderedDraft, renderedError, view],
  );

  const updateRawText = useCallback(
    (text: string) => {
      setRawText(text);
      const parsed = parseAgentEditConfigText(text, format);
      if (!parsed.ok) {
        setRawError(parsed.error);
        return;
      }
      setRawError(null);
      setDraftState(parsed.config);
      const nextRendered = renderedAgentEditDraft(parsed.config);
      setRenderedDraftState(nextRendered.draft);
      setRenderedError(nextRendered.error);
    },
    [format],
  );

  const selectFormat = useCallback(
    (nextFormat: CodeFormat) => {
      if (nextFormat === format || rawError) {
        return false;
      }
      setFormatState(nextFormat);
      setRawText(agentEditConfigText(draft, nextFormat));
      return true;
    },
    [draft, format, rawError],
  );

  const validateRawText = useCallback((text: string, nextFormat: CodeFormat) => {
    const parsed = parseAgentEditConfigText(text, nextFormat);
    return parsed.ok ? null : parsed.error;
  }, []);

  return {
    draft,
    renderedDraft,
    renderedError,
    renderedDraftError,
    setRenderedDraft,
    view,
    selectView,
    format,
    selectFormat,
    rawText,
    rawError,
    updateRawText,
    validateRawText,
  };
}
