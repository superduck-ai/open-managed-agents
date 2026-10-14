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
  return { id, file, mountPath: sessionAttachmentMountPath(id, file) };
}

function sessionAttachmentFilename(name: string) {
  const sanitized = name.replace(/[\\/<>:"|?*\p{Cc}]/gu, '_').trim();
  const filename = sanitized === '.' || sanitized === '..' ? 'file' : sanitized;
  return filename || 'file';
}

function sessionAttachmentMountPath(id: string, file: File) {
  return `attachments/${id}/${sessionAttachmentFilename(file.name)}`;
}

async function sessionAttachmentUploadFile(file: File) {
  const filename = sessionAttachmentFilename(file.name);
  return filename === file.name
    ? file
    : new File([await file.arrayBuffer()], filename, { type: file.type, lastModified: file.lastModified });
}

export async function prepareSessionAttachments(
  attachments: SessionAttachment[],
  sessionId: string,
  workspaceId: string,
  onResourcesChanged: () => void,
) {
  const paths: string[] = [];
  for (const attachment of attachments) {
    attachment.metadata ??= await anthropicBetaApi.files.upload<FileMetadataApiResponse>(
      await sessionAttachmentUploadFile(attachment.file),
      workspaceId,
    );
    if (!attachment.mounted) {
      attachment.mountPath = sessionAttachmentMountPath(attachment.id, attachment.file);
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
