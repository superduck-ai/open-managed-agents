import { expect, test } from 'bun:test';
import { feishuAppConsoleUrl } from './feishuConsole';

test('凭据后台链接指向该应用的基本信息页', () => {
  expect(feishuAppConsoleUrl('cli_aa6c7433e1b8dbf1', 'zh-CN')).toBe(
    'https://open.feishu.cn/app/cli_aa6c7433e1b8dbf1/baseinfo',
  );
  expect(feishuAppConsoleUrl(' cli_aa6c7433e1b8dbf1 ', 'en')).toBe(
    'https://open.larksuite.com/app/cli_aa6c7433e1b8dbf1/baseinfo',
  );
  expect(feishuAppConsoleUrl('', 'zh-CN')).toBeNull();
  expect(feishuAppConsoleUrl('cli_../admin', 'zh-CN')).toBeNull();
  expect(feishuAppConsoleUrl('https://open.feishu.cn', 'en')).toBeNull();
});
