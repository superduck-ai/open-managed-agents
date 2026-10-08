import { useCallback, useMemo, useState } from 'react';
import { createAgentConfigText, parseCreateAgentConfigText } from '../agentConfig';
import { type CodeFormat, type CreateAgentInput } from '../types';
import { createAgentDraftSchema, normalizeCreateAgentDraft, type CreateAgentView } from './create-dialog-model';

export function useCreateAgentDraft(initialDraft: CreateAgentInput) {
  const [draft, setDraftState] = useState(() => normalizeCreateAgentDraft(initialDraft));
  const [view, setViewState] = useState<CreateAgentView>('rendered');
  const [format, setFormatState] = useState<CodeFormat>('YAML');
  const [rawText, setRawText] = useState(() => createAgentConfigText(normalizeCreateAgentDraft(initialDraft), 'YAML'));
  const [rawError, setRawError] = useState<string | null>(null);
  const [replacementRevision, setReplacementRevision] = useState(0);

  const draftError = useMemo(() => {
    const result = createAgentDraftSchema.safeParse(draft);
    return result.success
      ? null
      : result.error.issues
          .slice(0, 3)
          .map((issue) => `${issue.path.join('.') || 'Agent configuration'}: ${issue.message}`)
          .join(' ');
  }, [draft]);

  const setDraft = useCallback((next: CreateAgentInput | ((current: CreateAgentInput) => CreateAgentInput)) => {
    setDraftState((current) => (typeof next === 'function' ? next(current) : next));
  }, []);

  const replaceDraft = useCallback(
    (next: CreateAgentInput) => {
      const normalized = normalizeCreateAgentDraft(next);
      setDraftState(normalized);
      setRawText(createAgentConfigText(normalized, format));
      setRawError(null);
      setReplacementRevision((current) => current + 1);
    },
    [format],
  );

  const selectView = useCallback(
    (nextView: CreateAgentView) => {
      if (nextView === view) {
        return true;
      }
      if (nextView === 'raw') {
        setRawText(createAgentConfigText(draft, format));
        setRawError(null);
        setViewState(nextView);
        return true;
      }
      if (rawError) {
        return false;
      }
      setViewState(nextView);
      return true;
    },
    [draft, format, rawError, view],
  );

  const updateRawText = useCallback(
    (text: string) => {
      setRawText(text);
      const parsed = parseCreateAgentConfigText(text, format);
      if (!parsed.ok) {
        setRawError(parsed.error);
        return;
      }
      setRawError(null);
      setDraftState(parsed.input);
    },
    [format],
  );

  const selectFormat = useCallback(
    (nextFormat: CodeFormat) => {
      if (nextFormat === format || rawError) {
        return false;
      }
      setFormatState(nextFormat);
      setRawText(createAgentConfigText(draft, nextFormat));
      return true;
    },
    [draft, format, rawError],
  );

  const validateRawText = useCallback((text: string, nextFormat: CodeFormat) => {
    const parsed = parseCreateAgentConfigText(text, nextFormat);
    return parsed.ok ? null : parsed.error;
  }, []);

  return {
    draft,
    setDraft,
    replaceDraft,
    view,
    selectView,
    format,
    selectFormat,
    rawText,
    rawError,
    updateRawText,
    validateRawText,
    draftError,
    replacementRevision,
  };
}
