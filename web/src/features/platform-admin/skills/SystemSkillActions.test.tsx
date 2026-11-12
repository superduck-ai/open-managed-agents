import { afterEach, expect, mock, test } from 'bun:test';
import type { ReactNode } from 'react';
import { renderPlatformAdmin } from '../../../test/platform-admin';
import { PlatformAdminClient } from '../../../shared/api/platform-admin';
import { PlatformAdminContext } from '../context';
import { SystemSkillActions } from './SystemSkillActions';
import { SystemSkillDeleteDialog } from './SystemSkillDeleteDialog';
import type { SystemSkill } from './types';

const { cleanup, fireEvent, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

const skill: SystemSkill = {
  id: 'demo',
  display_title: 'Demo',
  listed: true,
  default_version: '1',
  revision: 4,
  created_at: '2026-11-01',
  updated_at: '2026-11-01',
};

function renderActions(ui: ReactNode) {
  return renderPlatformAdmin(
    <PlatformAdminContext.Provider
      value={{
        client: new PlatformAdminClient(() => {}),
        bootstrap: {
          account: { uuid: 'actor', name: 'Actor', email: 'test@example.com' },
          platform_access: { role: 'super_admin', permissions: ['platform:access'] },
          csrf_token: 'test',
        },
      }}
    >
      {ui}
    </PlatformAdminContext.Provider>,
  );
}

test('预置技能可以更新和换图标，不能删除底本', async () => {
  renderActions(
    <SystemSkillActions
      skill={{ ...skill, bundled: true, bundled_version: '1' }}
      onUpdate={() => {}}
      onDelete={() => {}}
      onToggle={() => {}}
      onChanged={async () => {}}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Demo的更多操作' }));
  expect(await screen.findByRole('menuitem', { name: '下载技能' })).toBeTruthy();
  expect(screen.getByRole('menuitem', { name: '下架技能' })).toBeTruthy();
  expect(screen.getByRole('menuitem', { name: '更新技能' })).toBeTruthy();
  expect(screen.getByRole('menuitem', { name: '更换图标' })).toBeTruthy();
  expect(screen.queryByRole('menuitem', { name: '恢复预置版本' })).toBeNull();
  expect(screen.queryByRole('menuitem', { name: '删除技能' })).toBeNull();
});

test('上传技能的外层菜单保留更新、图标和删除', async () => {
  const remove = mock(() => {});
  renderActions(
    <SystemSkillActions
      skill={skill}
      onUpdate={() => {}}
      onDelete={remove}
      onToggle={() => {}}
      onChanged={async () => {}}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Demo的更多操作' }));
  expect(await screen.findByRole('menuitem', { name: '更新技能' })).toBeTruthy();
  expect(screen.getByRole('menuitem', { name: '更换图标' })).toBeTruthy();
  fireEvent.click(screen.getByRole('menuitem', { name: '删除技能' }));
  expect(remove).toHaveBeenCalledTimes(1);
});

test('确认删除提交当时的修订号，失败保留弹窗与错误', async () => {
  const closed = mock(() => {});
  const deleted = mock(async () => {});
  const conflict = mock(async () => {});
  let submitted: unknown;
  globalThis.fetch = mock(async (_url: RequestInfo | URL, options?: RequestInit) => {
    expect(options?.method).toBe('DELETE');
    submitted = JSON.parse(String(options?.body));
    return Response.json({ error: { code: 'revision_conflict', message: '技能已被更新' } }, { status: 409 });
  }) as typeof fetch;
  renderActions(<SystemSkillDeleteDialog skill={skill} onClose={closed} onDeleted={deleted} onConflict={conflict} />);
  const confirm = screen.getByRole('button', { name: '删除', exact: true }) as HTMLButtonElement;
  fireEvent.click(confirm);
  expect(await screen.findByText('技能已被更新')).toBeTruthy();
  expect(submitted).toEqual({ revision: 4 });
  expect(confirm.disabled).toBe(true);
  expect(conflict).toHaveBeenCalledTimes(1);
  expect(closed).not.toHaveBeenCalled();
  expect(deleted).not.toHaveBeenCalled();
});

test('删除成功后刷新列表并关闭确认', async () => {
  const closed = mock(() => {});
  const deleted = mock(async () => {});
  globalThis.fetch = mock(async () => new Response(null, { status: 204 })) as typeof fetch;
  renderActions(<SystemSkillDeleteDialog skill={skill} onClose={closed} onDeleted={deleted} />);
  fireEvent.click(screen.getByRole('button', { name: '删除', exact: true }));
  await waitFor(() => expect(deleted).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(closed).toHaveBeenCalledTimes(1));
});

test('覆盖版本提供恢复预置入口，确认后保留技能', async () => {
  const restore = mock(() => {});
  const overridden = { ...skill, bundled: true, bundled_version: 'bundled-base' };
  renderActions(
    <SystemSkillActions
      skill={overridden}
      onUpdate={() => {}}
      onDelete={restore}
      onToggle={() => {}}
      onChanged={async () => {}}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Demo的更多操作' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: '恢复预置版本' }));
  expect(restore).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole('menuitem', { name: '删除技能' })).toBeNull();
  cleanup();
  const closed = mock(() => {});
  const changed = mock(async () => {});
  globalThis.fetch = mock(async (_url: RequestInfo | URL, options?: RequestInit) => {
    expect(options?.method).toBe('DELETE');
    expect(JSON.parse(String(options?.body))).toEqual({ revision: overridden.revision });
    return new Response(null, { status: 204 });
  }) as typeof fetch;
  renderActions(<SystemSkillDeleteDialog skill={overridden} onClose={closed} onDeleted={changed} />);
  expect(screen.getByRole('alertdialog', { name: '恢复预置版本？' })).toBeTruthy();
  expect(screen.getByText('使用项目自带版本，保留上下架状态和已有会话。')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '恢复预置版本', exact: true }));
  await waitFor(() => expect(changed).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(closed).toHaveBeenCalledTimes(1));
});
