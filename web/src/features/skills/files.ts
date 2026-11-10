type SkillFile = File & {
  __skillPath?: string;
};

type SkillFileSystemEntry = {
  isFile: boolean;
  isDirectory: boolean;
  name: string;
  file?: (success: (file: File) => void, error?: (err: DOMException) => void) => void;
  createReader?: () => {
    readEntries: (success: (entries: SkillFileSystemEntry[]) => void, error?: (err: DOMException) => void) => void;
  };
};

export async function filesFromDataTransfer(dataTransfer: DataTransfer) {
  const entries = Array.from(dataTransfer.items ?? [])
    .map((item) => {
      const entry = (item as DataTransferItem & { webkitGetAsEntry?: () => unknown }).webkitGetAsEntry?.();
      return entry as SkillFileSystemEntry | null | undefined;
    })
    .filter((entry): entry is SkillFileSystemEntry => Boolean(entry));
  if (entries.length) {
    const nested = await Promise.all(entries.map((entry) => filesFromEntry(entry, '')));
    return nested.flat();
  }
  return Array.from(dataTransfer.files ?? []);
}

async function filesFromEntry(entry: SkillFileSystemEntry, parentPath: string): Promise<File[]> {
  const entryPath = `${parentPath}${entry.name}`;
  if (entry.isFile && entry.file) {
    const file = await new Promise<File>((resolve, reject) => {
      entry.file?.(resolve, reject);
    });
    (file as SkillFile).__skillPath = entryPath;
    return [file];
  }
  if (!entry.isDirectory || !entry.createReader) {
    return [];
  }
  const reader = entry.createReader();
  const children: SkillFileSystemEntry[] = [];
  for (;;) {
    const batch = await new Promise<SkillFileSystemEntry[]>((resolve, reject) => {
      reader.readEntries(resolve, reject);
    });
    if (!batch.length) {
      break;
    }
    children.push(...batch);
  }
  const nested = await Promise.all(children.map((child) => filesFromEntry(child, `${entryPath}/`)));
  return nested.flat();
}

export function skillFilePath(file: File) {
  return ((file as SkillFile).__skillPath || file.webkitRelativePath || file.name).replace(/\\/g, '/');
}

export function skillPackageBody(files: File[]) {
  const body = new FormData();
  for (const file of files) body.append('files[]', file, skillFilePath(file));
  return body;
}
