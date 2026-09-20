import { expect, test } from 'bun:test';

import { mergeDreamList, shouldPollDreamDetail, shouldPollDreamList } from './poll';
import type { Dream } from './api';

function dream(id: string, status: Dream['status']): Dream {
  return {
    id,
    type: 'dream',
    status,
    model: { id: 'claude-sonnet-4-6' },
    created_at: '2026-09-17T00:00:00Z',
    updated_at: '2026-09-17T00:00:00Z',
    inputs: [],
    outputs: [],
  };
}

test('polls the list only while the drawer history has a pending or running Dream', () => {
  const active = [dream('drm_running', 'running'), dream('drm_done', 'completed')];
  expect(shouldPollDreamList(true, 'history', active)).toBe(true);
  expect(shouldPollDreamList(true, 'history', [dream('drm_done', 'completed')])).toBe(false);
  expect(shouldPollDreamList(true, 'create', active)).toBe(false);
  expect(shouldPollDreamList(false, 'history', active)).toBe(false);
});

test('polls retrieve only while the open detail is still pending or running', () => {
  expect(shouldPollDreamDetail(true, 'detail', dream('drm_pending', 'pending'))).toBe(true);
  expect(shouldPollDreamDetail(true, 'detail', dream('drm_done', 'failed'))).toBe(false);
  expect(shouldPollDreamDetail(true, 'history', dream('drm_running', 'running'))).toBe(false);
});

test('merges the first list page onto already loaded rows without dropping later pages', () => {
  const first = dream('drm_new', 'pending');
  const updated = { ...dream('drm_running', 'completed'), usage: { input_tokens: 12 } };
  const older = dream('drm_old', 'completed');
  expect(mergeDreamList([dream('drm_running', 'running'), older], [first, updated])).toEqual([first, updated, older]);
});
