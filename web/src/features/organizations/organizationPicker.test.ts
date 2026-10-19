import { expect, test } from 'bun:test';
import {
  filterOrganizations,
  menuOrganizationPreview,
  organizationNameParts,
  organizationPickerRemainder,
  organizationPickerResultLimit,
  type OrganizationPickerItem,
} from './organizationPicker';

const items: OrganizationPickerItem[] = [
  { uuid: 'current', name: '集团-总部-财务中心-财务部', role: 'admin' },
  { uuid: 'other', name: '伙伴公司-星河-财务部', role: 'user' },
  ...Array.from({ length: 45 }, (_, index) => ({
    uuid: `shift-${index}`,
    name: `集团-制造部-一号工厂-甲班-${index}`,
    role: 'admin' as const,
  })),
];

test('组织名保留末级部门，路径包含五级及以下', () => {
  expect(organizationNameParts('集团-业务部-研发平台-平台中心-软件部')).toEqual({
    title: '软件部',
    detail: '平台中心 / 研发平台 / 业务部 / 集团',
  });
  expect(organizationNameParts('综合管理部')).toEqual({ title: '综合管理部', detail: '' });
});

test('未搜索时只保留当前组织', () => {
  const result = filterOrganizations(items, '  ', 'current');
  expect(result).toEqual({
    searching: false,
    matches: [items[0]],
    hiddenCount: items.length - 1,
  });
  expect(organizationPickerRemainder(result)).toBe(`还有 ${items.length - 1} 个组织，输入关键词查看。`);
});

test('按完整路径过滤，当前组织排在前面，超出上限的匹配被收起', () => {
  const result = filterOrganizations(items, '财务', 'other');
  expect(result.searching).toBe(true);
  expect(result.matches.map((item) => item.uuid)).toEqual(['other', 'current']);
  expect(result.hiddenCount).toBe(0);

  const shifts = filterOrganizations(items, '甲班', 'current');
  expect(shifts.matches).toHaveLength(organizationPickerResultLimit);
  expect(shifts.hiddenCount).toBe(45 - organizationPickerResultLimit);
  expect(organizationPickerRemainder(shifts)).toBe(`还有 ${shifts.hiddenCount} 个匹配，继续输入以缩小范围。`);
});

test('账号菜单默认只预览当前组织和另外两个', () => {
  const preview = menuOrganizationPreview(items, 'other');
  expect(preview.map((item) => item.uuid)).toEqual(['other', 'current', 'shift-0']);
  expect(menuOrganizationPreview(items.slice(0, 2), 'current')).toHaveLength(2);
});

test('没有匹配时不提示剩余数量', () => {
  const result = filterOrganizations(items, '不存在的部门', 'current');
  expect(result.matches).toEqual([]);
  expect(organizationPickerRemainder(result)).toBe('');
});
