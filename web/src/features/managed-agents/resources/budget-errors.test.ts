import { expect, test } from 'bun:test';
import { createIntl } from 'react-intl';
import zhMessages from '../../../shared/i18n/messages/zh-CN.json';
import { type I18nMsg } from '../types';
import { budgetErrorMessage } from './budget-errors';
const intl = createIntl({ locale: 'zh-CN', messages: zhMessages });
const msg: I18nMsg = (id, defaultMessage, values) => intl.formatMessage({ id, defaultMessage }, values);

test('预算错误保留未知错误并翻译参数校验和模型价格错误', () => {
  expect(budgetErrorMessage('network unavailable', msg)).toBe('network unavailable');
  expect(budgetErrorMessage("budget.max_list_cost must be greater than the session's consumed list cost", msg)).toBe(
    '新预算必须高于已使用的金额。',
  );
  expect(
    budgetErrorMessage('budget requires models with a list price; no list price configured for: model-a', msg),
  ).toBe('设置预算前，请先为以下模型配置价格：model-a');
  expect(budgetErrorMessage('budgets can only be attached when creating a session', msg)).toContain(
    '预算只能在创建会话时设置',
  );
});

test('预算已触顶时不重复显示协议错误', () => {
  const error = 'session budget reached: only settlement events (user.tool_result) are accepted';
  expect(budgetErrorMessage(error, msg, false)).toBe('会话已达到预算上限，请调整或移除预算后继续。');
  expect(budgetErrorMessage(error, msg, true)).toBeNull();
});
