export function acceptanceItems(value: string) {
  const items = value
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean);
  return items.length > 0 ? items : [''];
}

export function joinAcceptance(items: string[]) {
  return items
    .map((item) => item.trim())
    .filter(Boolean)
    .join('\n');
}
