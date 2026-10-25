import { useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { Button } from '../../shared/ui/button';

export function DatasetUploadDropzone({
  file,
  tooLarge,
  onFile,
  onHelp,
  onTemplate,
}: {
  file: File | null;
  tooLarge: boolean;
  onFile: (file: File | null) => void;
  onHelp: () => void;
  onTemplate: () => void;
}) {
  const { msg } = useI18n();
  const [dragging, setDragging] = useState(false);
  return (
    <div className="grid gap-2">
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
        className={`flex min-h-28 cursor-pointer flex-col items-center justify-center gap-1 rounded-lg border border-dashed px-4 py-6 text-center text-sm ${dragging ? 'border-primary bg-muted/50' : 'border-input'}`}
      >
        <span className="font-medium">{msg('evaluations.dropzone', 'Drag or choose a dataset file')}</span>
        <span className="text-muted-foreground">{file?.name ?? msg('evaluations.dropzoneHint', dropzoneHint)}</span>
      </label>
      <input
        id="evaluation-dataset-file"
        className="sr-only"
        type="file"
        accept=".jsonl,.zip,application/jsonl,application/zip"
        onChange={(event) => onFile(event.target.files?.[0] ?? null)}
      />
      {tooLarge ? (
        <p className="text-sm text-destructive">
          {msg('evaluations.importFileTooLarge', 'File must be 100MB or smaller.')}
        </p>
      ) : null}
      <p className="text-sm text-muted-foreground">{msg('evaluations.templateHelper', templateHelper)}</p>
      <div className="flex flex-wrap gap-3">
        <Button type="button" variant="link" className="h-auto px-0" onClick={onTemplate}>
          {msg('evaluations.downloadTemplate', 'Download template')}
        </Button>
        <Button type="button" variant="link" className="h-auto px-0" onClick={onHelp}>
          {msg('evaluations.createHelp', 'How to create a dataset')}
        </Button>
      </div>
    </div>
  );
}

const dropzoneHint = 'JSONL or ZIP, up to 100MB and 1000 cases. Put attachments in a ZIP.';
const templateHelper =
  'The ZIP template is a complete example: cases.jsonl plus sample input_files/ and reference_files/.';
