import { afterEach, expect, mock, test } from 'bun:test';
import { I18nProvider } from '../i18n';
import { resetTestDom } from '../../test/setup';
import { CreateWorkspaceDialog } from './CreateWorkspaceDialog';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
afterEach(cleanup);

test('保留名称显示明确原因，不提交创建请求', async () => {
  resetTestDom('https://oma.duck.ai/');
  const create = mock(async () => undefined);
  render(
    <I18nProvider initialLocale="zh-CN">
      <CreateWorkspaceDialog open onOpenChange={() => undefined} onCreate={create} />
    </I18nProvider>,
  );
  fireEvent.change(screen.getByLabelText(/^(名称|Name)$/), { target: { value: ' Default ' } });
  fireEvent.submit(screen.getByLabelText(/^(名称|Name)$/).closest('form')!);
  await waitFor(() =>
    expect(screen.getByRole('alert').textContent).toContain('Default 是默认工作区的保留名称，请使用其他名称。'),
  );
  expect(create).not.toHaveBeenCalled();
});

test('服务端返回的结构化错误展示具体原因', async () => {
  resetTestDom('https://oma.duck.ai/');
  const create = mock(async () => {
    throw { status: 409, message: 'Workspace name already exists' };
  });
  render(
    <I18nProvider initialLocale="en">
      <CreateWorkspaceDialog open onOpenChange={() => undefined} onCreate={create} />
    </I18nProvider>,
  );
  fireEvent.change(screen.getByLabelText(/^(名称|Name)$/), { target: { value: 'research' } });
  fireEvent.submit(screen.getByLabelText(/^(名称|Name)$/).closest('form')!);
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Workspace name already exists'));
});
