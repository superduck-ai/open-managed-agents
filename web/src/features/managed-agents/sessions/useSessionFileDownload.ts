import { useRef, useState } from 'react';
import { downloadApiFile } from '../../../shared/api/file-download';
import { toast } from '../../../shared/ui/sonner';
import { errorMessage } from '../utils';

export function useSessionFileDownload(workspaceId: string) {
  const [downloading, setDownloading] = useState(false);
  const busyRef = useRef(false);
  const download = async (file: { id: string; filename: string; downloadable?: boolean }) => {
    if (!file.downloadable || busyRef.current) return;
    busyRef.current = true;
    setDownloading(true);
    try {
      await downloadApiFile(file, workspaceId);
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      busyRef.current = false;
      setDownloading(false);
    }
  };
  return { download, downloading };
}
