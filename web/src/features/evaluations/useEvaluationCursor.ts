import { useState } from 'react';

type CursorState = {
  key: string;
  index: number;
  tokens: string[];
};

export function advanceEvaluationCursor(page: Pick<CursorState, 'index' | 'tokens'>, token: string) {
  const tokens = page.tokens.slice(0, page.index + 1);
  tokens[page.index + 1] = token;
  return { index: page.index + 1, tokens };
}

export function useEvaluationCursor(key: string) {
  const [state, setState] = useState<CursorState>({ key, index: 0, tokens: [] });
  const active = state.key === key ? state : { key, index: 0, tokens: [] };

  function previous() {
    setState((current) => {
      const page = current.key === key ? current : { key, index: 0, tokens: [] };
      return { ...page, index: Math.max(0, page.index - 1) };
    });
  }

  function next(token: string | null) {
    if (!token) return;
    setState((current) => {
      const page = current.key === key ? current : { key, index: 0, tokens: [] };
      return { key, ...advanceEvaluationCursor(page, token) };
    });
  }

  return { page: active.tokens[active.index], pageIndex: active.index, previous, next };
}
