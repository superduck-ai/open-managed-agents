type SkillFile = File & {
  __skillPath?: string;
};

export async function filesFromDataTransfer(dataTransfer: DataTransfer) {
  const entries = Array.from(dataTransfer.items ?? [])
    .map((item) => item.webkitGetAsEntry?.())
    .filter((entry): entry is FileSystemEntry => Boolean(entry));
  if (entries.length) {
    const nested = await Promise.all(entries.map((entry) => filesFromEntry(entry, '')));
    return nested.flat();
  }
  return Array.from(dataTransfer.files ?? []);
}

async function filesFromEntry(entry: FileSystemEntry, parentPath: string): Promise<File[]> {
  const entryPath = `${parentPath}${entry.name}`;
  if (entry.isFile) {
    const file = await new Promise<File>((resolve, reject) => {
      (entry as FileSystemFileEntry).file(resolve, reject);
    });
    (file as SkillFile).__skillPath = entryPath;
    return [file];
  }
  if (!entry.isDirectory) {
    return [];
  }
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  const children: FileSystemEntry[] = [];
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => {
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
