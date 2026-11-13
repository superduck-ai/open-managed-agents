import { useState } from 'react';
import { Download, LoaderCircle } from 'lucide-react';
import { toast } from 'sonner';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { errorMessage } from '@/features/dashboard/model';
import { downloadSkillPackage } from './api';

export function SkillDownloadButton({ url, workspaceId }: { url: string; workspaceId?: string }) {
  const { msg } = useI18n();
  const [pending, setPending] = useState(false);
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      aria-label={msg('skills.download', 'Download ZIP')}
      title={msg('skills.download', 'Download ZIP')}
      disabled={pending}
      onClick={async () => {
        setPending(true);
        try {
          await downloadSkillPackage(url, workspaceId);
        } catch (cause) {
          toast.error(errorMessage(cause));
        } finally {
          setPending(false);
        }
      }}
    >
      {pending ? (
        <LoaderCircle className="size-4 animate-spin" aria-hidden />
      ) : (
        <Download className="size-4" aria-hidden />
      )}
    </Button>
  );
}
