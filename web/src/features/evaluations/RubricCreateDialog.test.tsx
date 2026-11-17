import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { RubricCreateDialog } from './RubricCreateDialog';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');

afterEach(cleanup);

test('rubric dialog switches between generation and a written template', () => {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <I18nProvider initialLocale="zh-CN">
        <RubricCreateDialog open onOpenChange={() => undefined} />
      </I18nProvider>
    </QueryClientProvider>,
  );

  expect(screen.getByText('生成依据')).toBeTruthy();
  expect(screen.getByRole('button', { name: '生成评分标准' })).toBeTruthy();

  fireEvent.click(screen.getByRole('tab', { name: '手动编写' }));

  expect(screen.getByText('评分规则')).toBeTruthy();
  expect(screen.getByPlaceholderText(/# 评分标准/)).toBeTruthy();
  expect(screen.getByRole('button', { name: '创建' })).toBeTruthy();
});
