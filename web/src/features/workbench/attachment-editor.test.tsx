import { afterEach, expect, mock, test } from 'bun:test';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { resetTestDom } from '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { MessageEditor } from './editor';
import type { WorkbenchMessage } from './api';

const message: WorkbenchMessage = {
  role: 'human',
  content: [
    { type: 'text', text: 'Keep {{topic}} and this content unchanged.' },
    { type: 'document', title: 'Research Report.pdf', source: { type: 'file', file_id: 'file_sample' } },
  ],
};

afterEach(cleanup);

function renderEditor(isReadOnly = false) {
  resetTestDom('https://oma.duck.ai/workbench');
  const onAddUrl = mock(() => undefined);
  const onRemoveFile = mock(() => undefined);
  const onVariableClick = mock(() => undefined);
  render(
    <I18nProvider initialLocale="zh-CN">
      <MessageEditor
        message={message}
        index={0}
        messages={[message]}
        onChange={() => undefined}
        onRemove={() => undefined}
        onUpload={() => undefined}
        onAddUrl={onAddUrl}
        onReplaceFile={() => undefined}
        onRemoveFile={onRemoveFile}
        onVariableClick={onVariableClick}
        isReadOnly={isReadOnly}
      />
    </I18nProvider>,
  );
  return { onAddUrl, onRemoveFile, onVariableClick };
}

test('keeps an empty attachment URL unavailable and cancels without changing attachments', async () => {
  const { onAddUrl } = renderEditor();
  fireEvent.click(screen.getByRole('button', { name: '最多上传 100 个文件，每个不超过 20MB。' }));
  fireEvent.click(screen.getByRole('menuitem', { name: '图片' }));
  expect(screen.getByRole('menuitem', { name: '从设备上传' })).toBeTruthy();
  fireEvent.click(screen.getByRole('menuitem', { name: '通过链接添加' }));
  const dialog = await screen.findByRole('dialog', { name: '通过链接添加图片' });
  expect(within(dialog).getByRole('button', { name: '添加' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(within(dialog).getByRole('textbox', { name: 'URL' }), { target: { value: '   ' } });
  expect(within(dialog).getByRole('button', { name: '添加' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(within(dialog).getByRole('button', { name: '取消' }));
  expect(onAddUrl).not.toHaveBeenCalled();
  expect(screen.getByText('Research Report.pdf')).toBeTruthy();
});

test('localizes attachment actions while preserving file names, variables and URL payloads', async () => {
  const { onAddUrl, onRemoveFile, onVariableClick } = renderEditor();
  expect(screen.getByRole('button', { name: '预览 Research Report.pdf' }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByRole('button', { name: '替换 Research Report.pdf' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '设置 topic 的值' }));
  expect(onVariableClick).toHaveBeenCalledWith('topic');
  fireEvent.click(screen.getByRole('button', { name: '移除 Research Report.pdf' }));
  expect(onRemoveFile).toHaveBeenCalledWith(0, 1);
  fireEvent.click(screen.getByRole('button', { name: '最多上传 100 个文件，每个不超过 20MB。' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'PDF' }));
  fireEvent.click(screen.getByRole('menuitem', { name: '通过链接添加' }));
  const dialog = await screen.findByRole('dialog', { name: '通过链接添加 PDF' });
  fireEvent.change(within(dialog).getByRole('textbox', { name: 'URL' }), {
    target: { value: '  https://example.invalid/Report.pdf?name=Original  ' },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: '添加' }));
  expect(onAddUrl).toHaveBeenCalledWith(0, 'pdf', 'https://example.invalid/Report.pdf?name=Original');
  expect(screen.getByRole('textbox', { name: '用户提示词 1' }).textContent).toBe(message.content[0].text);
});

test('uses the localized upload limit for drag feedback and keeps read-only attachment actions unavailable', () => {
  renderEditor(true);
  expect(screen.getByRole('button', { name: '替换 Research Report.pdf' }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByRole('button', { name: '移除 Research Report.pdf' }).hasAttribute('disabled')).toBe(true);
  const card = document.querySelector('.workbench-message-card')!;
  fireEvent.dragEnter(card, { dataTransfer: { types: ['Files'] } });
  expect(card.classList.contains('is-drag-over')).toBe(false);
  cleanup();
  renderEditor();
  const editableCard = document.querySelector('.workbench-message-card')!;
  fireEvent.dragEnter(editableCard, { dataTransfer: { types: ['Files'] } });
  const feedback = document.querySelector('.workbench-message-drop-target')!;
  expect(feedback.getAttribute('aria-hidden')).toBe('false');
  expect(feedback.textContent).toContain('最多上传 100 个文件，每个不超过 20MB。');
  expect(feedback.textContent).not.toContain('Max 100 files');
});
