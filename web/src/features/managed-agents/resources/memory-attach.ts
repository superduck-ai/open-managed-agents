import type { ManagedEntityApiResponse, MemoryAttachFormValue } from '../types';

export const MAX_MEMORY_ATTACHES = 8;
export const MAX_MEMORY_ATTACH_INSTRUCTIONS = 500;

const FORBIDDEN_MEMORY_ATTACH_KEYS = ['mount_path', 'name', 'description'] as const;

export function memoryInstructionsCodePointCount(value: string) {
  return Array.from(value).length;
}

export function areMemoryAttachesValid(attaches: MemoryAttachFormValue[]) {
  if (attaches.length > MAX_MEMORY_ATTACHES) {
    return false;
  }
  const storeIds = new Set<string>();
  for (const attach of attaches) {
    if (!attach.memoryStoreId) {
      return false;
    }
    if (storeIds.has(attach.memoryStoreId)) {
      return false;
    }
    storeIds.add(attach.memoryStoreId);
    if (attach.access !== 'read_write' && attach.access !== 'read_only') {
      return false;
    }
    if (memoryInstructionsCodePointCount(attach.instructions) > MAX_MEMORY_ATTACH_INSTRUCTIONS) {
      return false;
    }
  }
  return true;
}

export function emptyMemoryAttach(): MemoryAttachFormValue {
  return {
    memoryStoreId: '',
    access: 'read_write',
    instructions: '',
  };
}

export function syncMemoryAttaches(current: MemoryAttachFormValue[], selectedIds: string[]): MemoryAttachFormValue[] {
  const byId = new Map(current.map((attach) => [attach.memoryStoreId, attach]));
  return selectedIds.map(
    (memoryStoreId) =>
      byId.get(memoryStoreId) ?? {
        memoryStoreId,
        access: 'read_write',
        instructions: '',
      },
  );
}

export function memoryAttachResources(attaches: MemoryAttachFormValue[]) {
  return attaches
    .filter((attach) => attach.memoryStoreId)
    .map((attach) => {
      const resource: {
        type: 'memory_store';
        memory_store_id: string;
        access: MemoryAttachFormValue['access'];
        instructions?: string;
      } = {
        type: 'memory_store',
        memory_store_id: attach.memoryStoreId,
        access: attach.access,
      };
      if (attach.instructions) {
        resource.instructions = attach.instructions;
      }
      return resource;
    });
}

export function entityMemoryAttaches(entity?: ManagedEntityApiResponse): MemoryAttachFormValue[] {
  if (!entity || !('resources' in entity) || !Array.isArray(entity.resources)) {
    return [];
  }
  const attaches: MemoryAttachFormValue[] = [];
  for (const resource of entity.resources) {
    if (!resource || typeof resource !== 'object') {
      continue;
    }
    const record = resource as Record<string, unknown>;
    if (record.type !== 'memory_store' || typeof record.memory_store_id !== 'string' || !record.memory_store_id) {
      continue;
    }
    attaches.push({
      memoryStoreId: record.memory_store_id,
      access: record.access === 'read_only' ? 'read_only' : 'read_write',
      instructions: typeof record.instructions === 'string' ? record.instructions : '',
      mountPath: typeof record.mount_path === 'string' ? record.mount_path : undefined,
    });
  }
  return attaches;
}

export function memoryAttachHasForbiddenClientFields(resource: object) {
  return FORBIDDEN_MEMORY_ATTACH_KEYS.some((key) => key in resource);
}
