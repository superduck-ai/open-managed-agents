import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { CaseAttachments, CaseInputMedia } from './caseMedia';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');

afterEach(cleanup);

test('case images render as thumbnails and documents use their filename', async () => {
  URL.createObjectURL = () => 'blob:case-image';
  URL.revokeObjectURL = () => undefined;
  render(
    <I18nProvider initialLocale="zh-CN">
      <CaseAttachments
        files={[
          { id: 'file_secret', kind: 'image' },
          { id: 'file_doc', kind: 'document' },
        ]}
        orgUuid="org"
        workspaceId="ws"
        onRemove={() => undefined}
        loadFile={async () => ({ blob: async () => new Blob(['x'], { type: 'image/png' }) })}
        loadName={async () => 'notes.pdf'}
      />
      <CaseInputMedia
        inputs={[{ type: 'image', source: { type: 'file', file_id: 'file_row' } }]}
        orgUuid="org"
        workspaceId="ws"
        loadFile={async () => ({ blob: async () => new Blob(['x'], { type: 'image/png' }) })}
      />
    </I18nProvider>,
  );

  await waitFor(() => expect(screen.getAllByRole('img', { name: '查看图片' }).length).toBe(2));
  expect(screen.queryByText('file_secret')).toBeNull();
  expect(screen.queryByText('file_doc')).toBeNull();
  expect(screen.getByText('notes.pdf')).toBeTruthy();

  fireEvent.click(screen.getAllByRole('img', { name: '查看图片' })[0]!);
  expect(screen.getByRole('heading', { name: '查看图片' })).toBeTruthy();
});
