import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { toast } from 'sonner';
import { renderPlatformAdmin } from '../../test/platform-admin';
import { SkillActions } from './SkillActions';

const { cleanup, fireEvent, screen, waitFor } = await import('@testing-library/react');
afterEach(cleanup);

test('菜单下载失败显示错误，不触发更新', async () => {
  const errorToast = spyOn(toast, 'error').mockImplementation(() => 'download-error');
  const onUpdate = mock(() => {});
  try {
    renderPlatformAdmin(
      <SkillActions
        name="Demo"
        onUpdate={onUpdate}
        onIcon={async () => {}}
        onDownload={async () => {
          throw new Error('下载失败');
        }}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Demo的更多操作' }));
    fireEvent.click(await screen.findByRole('menuitem', { name: '下载技能' }));
    await waitFor(() => expect(errorToast).toHaveBeenCalledWith('下载失败'));
    expect(onUpdate).not.toHaveBeenCalled();
  } finally {
    errorToast.mockRestore();
  }
});

test('菜单直接下载，完成后关闭，不触发编辑或删除', async () => {
  const onDownload = mock(async () => {});
  const onUpdate = mock(() => {});
  const onDelete = mock(() => {});
  renderPlatformAdmin(
    <SkillActions
      name="Demo"
      onUpdate={onUpdate}
      onDelete={onDelete}
      onIcon={async () => {}}
      onDownload={onDownload}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Demo的更多操作' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: '下载技能' }));
  await waitFor(() => expect(onDownload).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(screen.queryByRole('menu')).toBeNull());
  expect(onUpdate).not.toHaveBeenCalled();
  expect(onDelete).not.toHaveBeenCalled();
});
