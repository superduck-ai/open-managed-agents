import type { useI18n } from '@/shared/i18n';

export function skillErrorMessage(error: unknown, msg: ReturnType<typeof useI18n>['msg']) {
  const message =
    error instanceof Error ? error.message : String((error as { message?: string } | null)?.message ?? '');
  if (message.startsWith('Skill cannot reuse an existing display_title:')) {
    return msg(
      'skills.import.duplicateName',
      'A skill with this name already exists. Change the name or use Update skill.',
    );
  }
  return message;
}
