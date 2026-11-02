import type { useI18n } from '../i18n';

export function skillSourceLabel(source: string, msg?: ReturnType<typeof useI18n>['msg']) {
  const normalized = source.trim().toLowerCase();
  if (normalized === 'anthropic' || normalized === 'anthropic_example') {
    return msg ? msg('systemSkills.builtin', 'System built-in') : 'System built-in';
  }
  if (normalized === 'custom') {
    return msg ? msg('skills.source.custom', 'Custom') : 'Custom';
  }
}
