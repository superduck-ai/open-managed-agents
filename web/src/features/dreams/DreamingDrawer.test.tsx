import { afterEach, describe, expect, mock, test } from 'bun:test';

import { resetTestDom } from '../../test/setup';
import { Sheet } from '../../shared/ui/sheet';
import { DreamDetails } from './DreamingDrawer';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(() => {
  cleanup();
});

describe('DreamDetails', () => {
  test('shows the persisted error type, message, ended_at, and usage for a failed Dream', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores');

    render(
      <Sheet open onOpenChange={() => {}}>
        <DreamDetails
          dream={{
            id: 'drm_failed',
            type: 'dream',
            status: 'failed',
            model: { id: 'Qwen3.8-Max' },
            created_at: '2026-09-16T10:10:02Z',
            updated_at: '2026-09-16T10:10:02Z',
            ended_at: '2026-09-16T10:10:02Z',
            inputs: [
              { type: 'memory_store', memory_store_id: 'memstore_source' },
              { type: 'sessions', session_ids: [] },
            ],
            outputs: [],
            usage: {
              input_tokens: 1200,
              output_tokens: 30,
              cache_read_input_tokens: 0,
              cache_creation_input_tokens: 0,
            },
            error: {
              type: 'input_memory_store_unavailable',
              message: 'input memory store memstore_source was archived after the Dream was created',
            },
          }}
          stores={[]}
          sessions={[]}
          loading={false}
          error={null}
          onBack={() => {}}
          onArchive={async () => {}}
        />
      </Sheet>,
    );

    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain('输入记忆存储已被归档或删除');
    expect(alert.textContent).toContain('type: input_memory_store_unavailable');
    expect(alert.textContent).toContain('message: input memory store memstore_source was archived');
    expect(screen.getByText('结束时间')).toBeTruthy();
    expect(screen.getByText('1,200')).toBeTruthy();
    expect(screen.getByText('暂无产物。')).toBeTruthy();
  });

  test('shows the pending audit snapshot while the Worker prepares and starts Dream automatically', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores');
    const onArchive = mock(async () => {});
    const onCancel = mock(async () => {});

    render(
      <Sheet open onOpenChange={() => {}}>
        <DreamDetails
          dream={{
            id: 'drm_pending',
            type: 'dream',
            status: 'pending',
            model: { id: 'claude-sonnet-4-6' },
            instructions: '重点关注项目约定',
            created_at: '2026-09-13T10:00:00Z',
            updated_at: '2026-09-13T10:00:00Z',
            inputs: [
              { type: 'memory_store', memory_store_id: 'memstore_taste' },
              { type: 'sessions', session_ids: [] },
            ],
            outputs: [],
          }}
          stores={[
            {
              id: 'memstore_taste',
              type: 'memory_store',
              name: '口味偏好',
              description: '',
              created_at: '2026-09-13T10:00:00Z',
              updated_at: '2026-09-13T10:00:00Z',
              archived_at: null,
            },
          ]}
          sessions={[]}
          loading={false}
          error="详情刷新失败"
          onBack={() => {}}
          onArchive={onArchive}
          onCancel={onCancel}
        />
      </Sheet>,
    );

    expect(screen.getByText('pending')).toBeTruthy();
    expect(screen.getByText('口味偏好')).toBeTruthy();
    expect(screen.getByText('未选择；本次 Dream 的会话输入为空。')).toBeTruthy();
    expect(screen.getByText('重点关注项目约定')).toBeTruthy();
    expect(screen.getByText('等待执行，暂无产物。')).toBeTruthy();
    expect(screen.getByText(/后台 Dream Worker 会自动复用或创建默认环境/)).toBeTruthy();
    expect(screen.getByText('详情刷新失败')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByText('用量')).toBeNull();
    expect(screen.queryByText('结束时间')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: '取消' }));
    await waitFor(() => expect(onCancel).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('button', { name: '归档' })).toBeNull();
  });

  test('shows the running output Store and internal Session', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores');

    render(
      <Sheet open onOpenChange={() => {}}>
        <DreamDetails
          dream={{
            id: 'drm_running',
            type: 'dream',
            status: 'running',
            model: { id: 'claude-sonnet-4-6' },
            created_at: '2026-09-13T10:00:00Z',
            updated_at: '2026-09-13T10:01:00Z',
            session_id: 'sesn_internal',
            inputs: [
              { type: 'memory_store', memory_store_id: 'memstore_source' },
              { type: 'sessions', session_ids: ['sesn_source'] },
            ],
            outputs: [{ type: 'memory_store', memory_store_id: 'memstore_output' }],
            usage: {
              input_tokens: 42,
              output_tokens: 7,
              cache_read_input_tokens: 0,
              cache_creation_input_tokens: 0,
            },
          }}
          stores={[
            {
              id: 'memstore_source',
              type: 'memory_store',
              name: '输入记忆',
              description: '',
              created_at: '',
              updated_at: '',
              archived_at: null,
            },
          ]}
          sessions={[{ id: 'sesn_source', type: 'session', title: '输入会话' } as never]}
          loading={false}
          error={null}
          onBack={() => {}}
          onArchive={async () => {}}
        />
      </Sheet>,
    );

    expect(screen.getByText('Dream 正在整理记忆。')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'memstore_output' }).getAttribute('href')).toBe(
      '/workspaces/default/memory-stores/memstore_output',
    );
    expect(screen.getByRole('link', { name: 'sesn_internal' }).getAttribute('href')).toBe(
      '/workspaces/default/sessions/sesn_internal',
    );
    expect(screen.getByRole('button', { name: '取消' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: '归档' })).toBeNull();
    expect(screen.getByText('用量')).toBeTruthy();
    expect(screen.getByText('42')).toBeTruthy();
  });

  test('archives a completed Dream', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/memory-stores');
    const onArchive = mock(async () => {});

    render(
      <Sheet open onOpenChange={() => {}}>
        <DreamDetails
          dream={{
            id: 'drm_done',
            type: 'dream',
            status: 'completed',
            model: { id: 'claude-sonnet-4-6' },
            created_at: '2026-09-13T10:00:00Z',
            updated_at: '2026-09-13T10:02:00Z',
            session_id: 'sesn_internal',
            inputs: [
              { type: 'memory_store', memory_store_id: 'memstore_source' },
              { type: 'sessions', session_ids: [] },
            ],
            outputs: [{ type: 'memory_store', memory_store_id: 'memstore_output' }],
          }}
          stores={[]}
          sessions={[]}
          loading={false}
          error={null}
          onBack={() => {}}
          onArchive={onArchive}
        />
      </Sheet>,
    );

    fireEvent.click(screen.getByRole('button', { name: '归档' }));
    await waitFor(() => expect(onArchive).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('button', { name: '取消' })).toBeNull();
  });
});
