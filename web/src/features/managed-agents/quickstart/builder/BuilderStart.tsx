import type { ReactNode } from 'react';
import { Search, FileJson2, Globe, LifeBuoy, ChartNoAxesCombined, Code2, ArrowRight } from 'lucide-react';
import { Button } from '../../../../shared/ui/button';
import { useI18n, type Locale } from '../../../../shared/i18n';
import { quickstartAgentBody, quickstartDraft } from '../model';
import type { Builder } from './useBuilder';

function startTemplates(locale: Locale, modelId: string) {
  const zh = locale === 'zh-CN';
  const extract = quickstartAgentBody({
    name: zh ? '结构化提取' : 'Structured extractor',
    description: zh ? '把邮件、文档和文字转成 JSON。' : 'Turn emails, documents and text into JSON.',
    system: zh
      ? '按用户提供的字段提取信息。缺失值填 null，只输出合法 JSON，不猜测。'
      : 'Extract the fields requested by the user. Use null for missing values. Return only valid JSON without guessing.',
    modelID: modelId,
  });
  const support = quickstartAgentBody({
    name: zh ? '客服助手' : 'Support agent',
    description: zh
      ? '依据知识库回答，未知问题交给人工。'
      : 'Answer from your knowledge base and hand unknowns to a person.',
    system: zh
      ? '依据用户提供的资料回答客户问题。没有依据时说明未知，先生成回复草稿，不直接外发。'
      : 'Answer customer questions using user-provided material. Acknowledge unknowns. Draft replies for human review; do not send messages.',
    modelID: modelId,
  });
  return [
    {
      id: 'research',
      icon: Search,
      config: quickstartAgentBody(quickstartDraft('research', locale, modelId)),
      description: zh ? '研究一个问题，整理有来源的结论。' : 'Research a question and bring together sourced findings.',
    },
    { id: 'extract', icon: FileJson2, config: extract, description: extract.description },
    {
      id: 'tracking',
      icon: Globe,
      config: quickstartAgentBody(quickstartDraft('tracking', locale, modelId)),
      description: zh ? '收集近期变化，形成一份简报。' : 'Collect recent changes into a concise briefing.',
    },
    { id: 'support', icon: LifeBuoy, config: support, description: support.description },
    {
      id: 'analysis',
      icon: ChartNoAxesCombined,
      config: quickstartAgentBody(quickstartDraft('analysis', locale, modelId)),
      description: zh ? '分析表格，用数据回答问题。' : 'Analyze tables and answer questions with data.',
    },
    {
      id: 'review',
      icon: Code2,
      config: quickstartAgentBody(quickstartDraft('review', locale, modelId)),
      description: zh ? '检查代码，只报告能确认的问题。' : 'Review code and report only verifiable problems.',
    },
  ];
}
export function BuilderStart({
  builder,
  modeControl,
  composer,
}: {
  builder: Builder;
  modeControl?: ReactNode;
  composer: ReactNode;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const model = builder.state.config.model;
  const templates = startTemplates(locale, typeof model === 'string' ? model : model.id);
  return (
    <section aria-labelledby="quickstart-start-title" className="min-h-0 min-w-0 flex-1 overflow-y-auto px-1 pb-12">
      <div className="mx-auto flex w-full max-w-[880px] flex-col items-center pt-[clamp(2rem,7vh,5rem)]">
        <h1 id="quickstart-start-title" className="mb-6 text-center text-2xl font-semibold">
          {zh ? '你想构建什么？' : 'What would you like to build?'}
        </h1>
        <div className="mb-6">{modeControl}</div>
        <div className="w-full max-w-[728px]">{composer}</div>
        <div
          aria-label={zh ? '快速开始模板' : 'Quickstart templates'}
          className="mt-10 grid w-full grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3"
        >
          {templates.map(({ id, icon: Icon, config, description }) => (
            <Button
              key={id}
              aria-label={config.name}
              aria-describedby={`quickstart-template-${id}-description`}
              variant="ghost"
              className="h-auto min-h-[150px] w-full min-w-0 flex-col items-start justify-start gap-3 rounded-xl border-[0.5px] border-border p-5 text-left font-normal whitespace-normal"
              disabled={builder.busy}
              onClick={() => void builder.startTemplate(config)}
            >
              <Icon aria-hidden="true" className="size-5 text-muted-foreground" strokeWidth={1.5} />
              <span className="font-medium">{config.name}</span>
              <span id={`quickstart-template-${id}-description`} className="text-sm leading-5 text-muted-foreground">
                {description}
              </span>
            </Button>
          ))}
        </div>
        <Button
          variant="link"
          className="mt-7 font-normal text-muted-foreground"
          disabled={builder.busy}
          onClick={() =>
            void builder.reply(
              zh ? '从空白开始，先问我想实现什么。' : 'Start from scratch. Ask what I want to build first.',
            )
          }
        >
          {zh ? '从空白智能体开始' : 'Start from scratch'}
          <ArrowRight aria-hidden="true" />
        </Button>
      </div>
    </section>
  );
}
