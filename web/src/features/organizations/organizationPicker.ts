export const organizationPickerResultLimit = 40;
export const organizationMenuPreviewLimit = 3;

export type OrganizationPickerItem = {
  uuid: string;
  name: string;
  role?: string;
};

export type OrganizationPickerResult = {
  searching: boolean;
  matches: OrganizationPickerItem[];
  hiddenCount: number;
};

export function organizationNameParts(name: string) {
  const segments = name
    .split('-')
    .filter((segment) => segment.length > 0)
    .reduce<string[]>((levels, segment) => {
      const previous = levels.at(-1);
      if (previous && /^\d+$/.test(segment)) {
        levels[levels.length - 1] = `${previous}-${segment}`;
        return levels;
      }
      levels.push(segment);
      return levels;
    }, []);
  if (segments.length <= 1) return { title: name, detail: '' };
  return { title: segments[segments.length - 1], detail: segments.slice(0, -1).reverse().join(' / ') };
}

export function menuOrganizationPreview(items: OrganizationPickerItem[], currentUuid?: string) {
  const current = items.find((item) => item.uuid === currentUuid);
  const others = items.filter((item) => item.uuid !== current?.uuid);
  return [...(current ? [current] : []), ...others].slice(0, organizationMenuPreviewLimit);
}

export function filterOrganizations(
  items: OrganizationPickerItem[],
  query: string,
  currentUuid?: string,
): OrganizationPickerResult {
  const normalized = query.trim().toLowerCase();
  if (!normalized) {
    const current = items.find((item) => item.uuid === currentUuid);
    return {
      searching: false,
      matches: current ? [current] : [],
      hiddenCount: Math.max(0, items.length - (current ? 1 : 0)),
    };
  }
  const matches = items
    .filter((item) => item.name.toLowerCase().includes(normalized))
    .sort((left, right) => {
      if (left.uuid === currentUuid) return -1;
      if (right.uuid === currentUuid) return 1;
      return left.name.localeCompare(right.name, 'zh');
    });
  return {
    searching: true,
    matches: matches.slice(0, organizationPickerResultLimit),
    hiddenCount: Math.max(0, matches.length - organizationPickerResultLimit),
  };
}

export function organizationPickerRemainder(result: OrganizationPickerResult) {
  if (result.hiddenCount <= 0) return '';
  if (!result.searching) return `还有 ${result.hiddenCount} 个组织，输入关键词查看。`;
  return `还有 ${result.hiddenCount} 个匹配，继续输入以缩小范围。`;
}
