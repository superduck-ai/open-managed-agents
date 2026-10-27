import { Upload } from 'lucide-react';
import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';

export function DatasetUploadDropzone({
  file,
  tooLarge,
  onFile,
  onTemplate,
}: {
  file: File | null;
  tooLarge: boolean;
  onFile: (file: File | null) => void;
  onTemplate: () => void;
}) {
  const { msg } = useI18n();
  const [dragging, setDragging] = useState(false);
  const title = msg('evaluations.dropzone', 'Drag or choose a dataset file');
  return (
    <div className="grid gap-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-0.5">
          <span className="text-destructive" aria-hidden>
            *
          </span>
          <span className="text-sm font-medium">{msg('evaluations.uploadFile', 'Upload file')}</span>
        </div>
        <Button type="button" variant="ghost" size="sm" className="text-muted-foreground" onClick={onTemplate}>
          <Upload className="size-4" aria-hidden />
          {msg('evaluations.downloadTemplate', 'Download template')}
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        {msg(
          'evaluations.templateHelper',
          'The ZIP template is a complete example: cases.jsonl plus sample input_files/ and reference_files/.',
        )}
      </p>
      <label
        htmlFor="evaluation-dataset-file"
        onDragEnter={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragOver={(event) => {
          event.preventDefault();
          setDragging(true);
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(event) => {
          event.preventDefault();
          setDragging(false);
          onFile(event.dataTransfer.files?.[0] ?? null);
        }}
        className={`flex min-h-44 cursor-pointer flex-col items-center justify-center gap-3 rounded-lg border border-dashed px-6 py-8 text-center ${dragging ? 'border-primary bg-muted/40' : 'border-input'}`}
      >
        <span className="flex size-9 items-center justify-center rounded-lg border bg-background shadow-xs">
          <Upload className="size-4 text-muted-foreground" aria-hidden />
        </span>
        <span className="grid gap-1">
          <span className="text-sm font-medium">{file?.name ?? title}</span>
          <span className="max-w-md text-sm text-muted-foreground">
            {msg('evaluations.dropzoneHint', dropzoneHint)}
          </span>
        </span>
      </label>
      <input
        id="evaluation-dataset-file"
        className="sr-only"
        type="file"
        accept=".jsonl,.zip,application/jsonl,application/zip"
        aria-label={title}
        onChange={(event) => onFile(event.target.files?.[0] ?? null)}
      />
      {tooLarge ? (
        <p className="text-sm text-destructive">
          {msg('evaluations.importFileTooLarge', 'File must be 100MB or smaller.')}
        </p>
      ) : null}
    </div>
  );
}

const dropzoneHint = 'JSONL or ZIP, up to 100MB and 1000 cases. Put attachments in a ZIP.';
