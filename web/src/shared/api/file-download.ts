import { anthropicBetaApi } from './anthropic';

export async function downloadApiFile(file: { id: string; filename: string }, workspaceId: string) {
  const response = await anthropicBetaApi.files.download(file.id, workspaceId);
  const url = URL.createObjectURL(await response.blob());
  const anchor = document.createElement('a');
  try {
    anchor.href = url;
    anchor.download = file.filename;
    document.body.appendChild(anchor);
    anchor.click();
  } finally {
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
}
