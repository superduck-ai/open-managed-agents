import { useEffect, useRef, useState } from 'react';

export function usePromptOptimization(
  optimize: (text: string, signal: AbortSignal) => Promise<string>,
  draft: string,
  setDraft: (text: string) => void,
) {
  const controller = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [original, setOriginal] = useState<string | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  const cancel = () => {
    controller.current?.abort();
    controller.current = null;
    setBusy(false);
  };
  const edit = (text: string) => {
    cancel();
    setOriginal(null);
    setError('');
    setDraft(text);
  };
  const start = async () => {
    if (controller.current || !draft.trim()) return;
    const request = new AbortController();
    controller.current = request;
    setBusy(true);
    setError('');
    try {
      const result = await optimize(draft, request.signal);
      if (request.signal.aborted) return;
      setOriginal(draft);
      setDraft(result);
    } catch (cause) {
      if (!request.signal.aborted) setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      if (controller.current === request) {
        controller.current = null;
        setBusy(false);
      }
    }
  };
  return { busy, error, original, start, cancel, edit, undo: () => original !== null && edit(original) };
}
