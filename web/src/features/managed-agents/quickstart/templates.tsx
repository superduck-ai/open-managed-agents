import { Search, FileJson2, Globe, LifeBuoy, ChartNoAxesCombined, Code2, type LucideIcon } from 'lucide-react';
import type { Locale } from '../../../shared/i18n';
import { quickstartAgentBody, quickstartDraft } from './model';

export const quickstartTemplateCard =
  'relative flex h-auto min-h-[150px] w-full min-w-0 flex-col items-start justify-start gap-3 rounded-xl border-[0.5px] border-border p-5 text-left text-sm font-normal whitespace-normal';
export const quickstartTemplateGrid = 'grid w-full grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3';

export function quickstartTemplates(locale: Locale, modelId: string) {
  const zh = locale === 'zh-CN';

  return [
    {
      id: 'research' as const,
      icon: Search,
      config: quickstartAgentBody(quickstartDraft('research', locale, modelId)),
      description: zh ? '研究一个问题，整理有来源的结论。' : 'Research a question and bring together sourced findings.',
    },
    {
      id: 'extract' as const,
      icon: FileJson2,
      config: quickstartAgentBody(quickstartDraft('extract', locale, modelId)),
      description: zh ? '把邮件、文档和文字转成 JSON。' : 'Turn emails, documents and text into JSON.',
    },
    {
      id: 'tracking' as const,
      icon: Globe,
      config: quickstartAgentBody(quickstartDraft('tracking', locale, modelId)),
      description: zh ? '收集近期变化，形成一份简报。' : 'Collect recent changes into a concise briefing.',
    },
    {
      id: 'support' as const,
      icon: LifeBuoy,
      config: quickstartAgentBody(quickstartDraft('support', locale, modelId)),
      description: zh
        ? '依据知识库回答，未知问题交给人工。'
        : 'Answer from your knowledge base and hand unknowns to a person.',
    },
    {
      id: 'analysis' as const,
      icon: ChartNoAxesCombined,
      config: quickstartAgentBody(quickstartDraft('analysis', locale, modelId)),
      description: zh ? '分析表格，用数据回答问题。' : 'Analyze tables and answer questions with data.',
    },
    {
      id: 'review' as const,
      icon: Code2,
      config: quickstartAgentBody(quickstartDraft('review', locale, modelId)),
      description: zh ? '检查代码，只报告能确认的问题。' : 'Review code and report only verifiable problems.',
    },
  ];
}

export function QuickstartTemplateContent({
  icon: Icon,
  title,
  description,
}: {
  icon: LucideIcon;
  title: string;
  description?: string;
}) {
  return (
    <>
      <Icon aria-hidden="true" className="size-5 text-muted-foreground" strokeWidth={1.5} />
      <span className="font-medium">{title}</span>
      <span className="text-sm leading-5 text-muted-foreground">{description}</span>
    </>
  );
}
