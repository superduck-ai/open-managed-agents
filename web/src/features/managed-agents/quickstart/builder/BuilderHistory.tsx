import { z } from 'zod';
import { BuilderAvatar } from './BuilderAvatar';
import { Bot, Check, Layers } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { MarkdownTranscriptContent } from '../../sessions/SessionTranscriptContent';
import { builderHistoryText, type BuilderMessage } from './model';

const savedResourceSchema = z.object({ id: z.string(), name: z.string(), version: z.number().optional() });
const savedResultSchema = z.object({
  saved: z.boolean().optional(),
  recovered: z.boolean().optional(),
  agent: savedResourceSchema.nullish(),
  environment: savedResourceSchema.nullish(),
  deployment: savedResourceSchema.nullish(),
  vaults: z.array(savedResourceSchema).optional(),
  session_id: z.string().optional(),
});

function resultContent(message: BuilderMessage): unknown {
  if (typeof message.content === 'string') return null;
  const block = message.content.find((item) => item.type === 'tool_result');
  if (!block) return null;
  try {
    return JSON.parse(String(block.content));
  } catch {
    return null;
  }
}

export function BuilderHistory({
  messages,
  pendingToolId,
  busy = false,
}: {
  messages: BuilderMessage[];
  pendingToolId?: string;
  busy?: boolean;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const latestAssistant = messages.reduce(
    (latest, message, index) =>
      message.role === 'assistant' && builderHistoryText(message, zh, pendingToolId) ? index : latest,
    -1,
  );
  return messages.map((message, index) => {
    const content = resultContent(message);
    const result = savedResultSchema.safeParse(content);
    if (result.success && (result.data.saved || result.data.recovered)) {
      return <SavedResult key={index} saved={result.data} />;
    }
    const text = builderHistoryText(message, zh, pendingToolId);
    if (!text) return null;
    const hasToolResult =
      Array.isArray(message.content) && message.content.some((block) => block.type === 'tool_result');
    const isUser =
      message.role === 'user' &&
      (!hasToolResult || Array.isArray(content) || z.object({ user_choice: z.string() }).safeParse(content).success);
    return (
      <article
        key={index}
        aria-label={isUser ? (zh ? '用户消息' : 'User message') : zh ? '助手消息' : 'Assistant message'}
        className={
          isUser
            ? 'ml-auto w-fit max-w-[90%] rounded-2xl bg-accent px-4 py-3 text-sm leading-6'
            : 'flex min-w-0 items-start gap-3 text-sm leading-6'
        }
      >
        {!isUser &&
          (!busy && index === latestAssistant ? (
            <BuilderAvatar state="idle" />
          ) : (
            <span aria-hidden="true" className="size-8 shrink-0" />
          ))}
        {isUser ? (
          <p className="break-words whitespace-pre-wrap">{text}</p>
        ) : (
          <div className="min-w-0 flex-1">
            <MarkdownTranscriptContent value={text} />
          </div>
        )}
      </article>
    );
  });
}

function SavedResult({ saved }: { saved: z.infer<typeof savedResultSchema> }) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';

  const resources = [saved.agent, saved.environment, saved.deployment, ...(saved.vaults ?? [])].filter(
    (item) => item != null,
  );
  return (
    <div role="group" aria-label={zh ? '保存结果' : 'Saved result'} className="space-y-2">
      {resources.length ? (
        resources.map((resource) => {
          const Icon = resource.version == null ? Layers : Bot;
          return (
            <div
              key={resource.id}
              className="flex min-w-0 items-center gap-3 rounded-xl border-[0.5px] border-border bg-card px-4 py-3"
            >
              <Icon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1 space-y-1">
                <p className="break-words text-sm font-medium">{resource.name}</p>
                <p className="text-xs text-muted-foreground">
                  {zh ? '已保存' : 'Saved'}
                  {resource.version != null && <span className="ml-2 font-mono">v{resource.version}</span>}
                </p>
              </div>
              <Check aria-hidden="true" className="size-4 shrink-0 text-success" />
            </div>
          );
        })
      ) : (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <Check aria-hidden="true" className="size-4 text-success" />
          {saved.session_id
            ? zh
              ? '测试会话：已创建'
              : 'Test session: created'
            : zh
              ? '配置已保存'
              : 'Configuration saved'}
        </p>
      )}
    </div>
  );
}
