import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { addSessionFileResource } from '../api';
import type { FileMetadataApiResponse } from '../types';
import { sessionFileRuntimePath } from './file-resource-path';

export type SessionAttachment = {
  id: string;
  file: File;
  metadata?: FileMetadataApiResponse;
  mounted?: boolean;
  mountPath: string;
};

export function createSessionAttachment(file: File): SessionAttachment {
  const id = crypto.randomUUID();
  const sanitized = file.name.replace(/[\\/]/g, '_').trim();
  const filename = sanitized === '.' || sanitized === '..' ? 'file' : sanitized;
  return { id, file, mountPath: `attachments/${id}/${filename || 'file'}` };
}

export async function prepareSessionAttachments(
  attachments: SessionAttachment[],
  sessionId: string,
  workspaceId: string,
  onResourcesChanged: () => void,
) {
  const paths: string[] = [];
  for (const attachment of attachments) {
    attachment.metadata ??= await anthropicBetaApi.files.upload<FileMetadataApiResponse>(attachment.file, workspaceId);
    if (!attachment.mounted) {
      await addSessionFileResource(
        sessionId,
        { fileId: attachment.metadata.id, mountPath: attachment.mountPath },
        workspaceId,
      );
      attachment.mounted = true;
      onResourcesChanged();
    }
    paths.push(sessionFileRuntimePath(attachment.mountPath));
  }
  return paths;
}

export function sessionAttachmentMessage(message: string, paths: string[]) {
  return [message, ...paths].filter(Boolean).join('\n\n');
}
