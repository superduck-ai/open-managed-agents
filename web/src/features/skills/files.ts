type SkillFile = File & {
  __skillPath?: string;
};

export function pickSkillFolder(): Promise<File[]> | null {
  const pickerWindow = window as Window & {
    showDirectoryPicker?: (options: { mode: 'read' }) => Promise<FileSystemDirectoryHandle>;
  };
  if (!pickerWindow.showDirectoryPicker) return null;
  return pickerWindow
    .showDirectoryPicker({ mode: 'read' })
    .then((directory) => filesFromDirectoryHandle(directory, directory.name));
}

async function filesFromDirectoryHandle(directory: FileSystemDirectoryHandle, path: string): Promise<File[]> {
  const files: File[] = [];
  for await (const handle of directory.values()) {
    const filePath = `${path}/${handle.name}`;
    if (handle.kind === 'directory') {
      files.push(...(await filesFromDirectoryHandle(handle, filePath)));
    } else {
      const file = await handle.getFile();
      (file as SkillFile).__skillPath = filePath;
      files.push(file);
    }
  }
  return files;
}

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
