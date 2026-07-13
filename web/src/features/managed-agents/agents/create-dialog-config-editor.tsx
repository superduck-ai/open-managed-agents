import { useI18n } from '../../../shared/i18n';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../../../shared/ui/tabs';
import { AgentConfigEditor } from '../components/AgentConfigEditor';
import { CopyButton } from '../components/CodeBlocks';
import { type CodeFormat } from '../types';

export function CreateDialogConfigEditor({
  format,
  configText,
  configError,
  onFormatChange,
  onEditorChange,
  validateEditorText,
}: {
  format: CodeFormat;
  configText: string;
  configError: string | null;
  onFormatChange: (format: CodeFormat) => void;
  onEditorChange: (value: string) => void;
  validateEditorText: (text: string, format: CodeFormat) => string | null;
}) {
  const { msg } = useI18n();

  return (
    <div className="mt-5 flex min-h-0 flex-1 basis-[304px] flex-col">
      <div className="text-sm font-semibold leading-5 text-foreground">
        {msg('managedAgents.agents.createDialog.agentConfig', 'Agent config')}
      </div>
      <div className="mt-3 flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border/70 bg-popover shadow-sm">
        <Tabs
          value={format}
          onValueChange={(nextValue) => nextValue && onFormatChange(nextValue as CodeFormat)}
          className="h-full gap-0"
        >
          <div className="flex h-11 items-center justify-between border-b border-border/60 px-3">
            <TabsList
              aria-label={msg('managedAgents.agents.createDialog.configFormat', 'Config format')}
              className="h-8"
            >
              <TabsTrigger value="YAML" className="px-4 text-[14px] font-semibold">
                YAML
              </TabsTrigger>
              <TabsTrigger value="JSON" className="px-4 text-[14px] font-semibold">
                JSON
              </TabsTrigger>
            </TabsList>
            <CopyButton value={configText} label={msg('managedAgents.quickstart.copyCode', 'Copy code')} />
          </div>
          <ConfigEditorTab
            format="YAML"
            activeFormat={format}
            configText={configText}
            onEditorChange={onEditorChange}
            validateEditorText={validateEditorText}
          />
          <ConfigEditorTab
            format="JSON"
            activeFormat={format}
            configText={configText}
            onEditorChange={onEditorChange}
            validateEditorText={validateEditorText}
          />
        </Tabs>
      </div>
      {configError ? <p className="mt-2 text-sm text-destructive">{configError}</p> : null}
    </div>
  );
}

function ConfigEditorTab({
  format,
  activeFormat,
  configText,
  onEditorChange,
  validateEditorText,
}: {
  format: CodeFormat;
  activeFormat: CodeFormat;
  configText: string;
  onEditorChange: (value: string) => void;
  validateEditorText: (text: string, format: CodeFormat) => string | null;
}) {
  return (
    <TabsContent value={format} className="mt-0 flex min-h-0 flex-1 flex-col overflow-hidden">
      {activeFormat === format ? (
        <div className="min-h-0 flex-1 overflow-hidden">
          <AgentConfigEditor
            value={configText}
            format={format}
            onChange={onEditorChange}
            validate={validateEditorText}
            minHeight="0px"
          />
        </div>
      ) : null}
    </TabsContent>
  );
}
