import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo } from 'react';
import { listSessionFiles, retrieveFileMetadata } from '../api';
import type { FileMetadataApiResponse, SessionApiResponse, SessionResourceApiResponse } from '../types';
import { sessionStatusIsLive } from './sessionDetailModel';

export type SessionFileSummary = { name: string; size: number; downloadable?: boolean };

export function sessionFilesQueryKey(workspaceId: string, sessionId: string) {
  return ['files', workspaceId, 'session', sessionId] as const;
}

function sessionFileMetadataQueryKey(workspaceId: string, resource: SessionResourceApiResponse) {
  return ['files', workspaceId, 'metadata', resource.file_id, resource.updated_at ?? ''] as const;
}

export function useSessionFileMetadata(session: SessionApiResponse, workspaceId: string) {
  const resources = [
    ...new Map(
      session.resources.filter((resource) => resource.file_id).map((resource) => [resource.file_id!, resource]),
    ).values(),
  ];
  const queries = useQueries({
    queries: resources.map((resource) => ({
      queryKey: sessionFileMetadataQueryKey(workspaceId, resource),
      queryFn: () => retrieveFileMetadata(resource.file_id!, workspaceId),
      staleTime: 15000,
      retry: false,
    })),
  });
  return Object.fromEntries(
    queries.flatMap(({ data }) =>
      data
        ? [
            [
              data.id,
              {
                name: data.filename,
                size: data.size_bytes,
                downloadable: data.downloadable,
              },
            ],
          ]
        : [],
    ),
  ) as Record<string, SessionFileSummary>;
}

export function sessionOutputFiles(files: FileMetadataApiResponse[]) {
  return [...new Map(files.filter((file) => file.downloadable === true).map((file) => [file.id, file])).values()].sort(
    (left, right) => Date.parse(right.created_at) - Date.parse(left.created_at),
  );
}

export function useSessionOutputFiles(session: SessionApiResponse, workspaceId: string, refreshKey: number) {
  const queryClient = useQueryClient();
  const running = sessionStatusIsLive(session.status) && !session.archived_at;
  const query = useQuery({
    queryKey: sessionFilesQueryKey(workspaceId, session.id),
    queryFn: ({ signal }) => listSessionFiles(session.id, workspaceId, signal),
    refetchInterval: running ? 3000 : false,
    refetchOnWindowFocus: true,
    retry: false,
  });
  const { data, refetch } = query;
  const signature = session.resources
    .map((resource) => `${resource.file_id ?? ''}:${resource.updated_at ?? ''}`)
    .join('|');
  useEffect(() => {
    void refetch();
  }, [refetch, refreshKey, session.status, session.archived_at, signature]);
  useEffect(() => {
    if (!data) return;
    const filesById = new Map(data.data.map((file) => [file.id, file]));
    for (const resource of session.resources) {
      const file = filesById.get(resource.file_id ?? '');
      if (file) queryClient.setQueryData(sessionFileMetadataQueryKey(workspaceId, resource), file);
    }
  }, [data, queryClient, session.resources, workspaceId]);
  const files = useMemo(() => sessionOutputFiles(data?.data ?? []), [data]);
  return { files, error: query.error, refresh: refetch };
}
