import { getScopeSignal } from './client';
import { anthropicBetaApi } from './anthropic';

export async function downloadApiFile(file: { id: string; filename: string }, workspaceId: string) {
  const scopeSignal = getScopeSignal();
  const response = await anthropicBetaApi.files.download(file.id, workspaceId);
  const blob = await response.blob();
  scopeSignal.throwIfAborted();
  const url = URL.createObjectURL(blob);
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
