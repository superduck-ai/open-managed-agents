import type { ReactNode } from 'react';
import { ArrowRight } from 'lucide-react';
import { Button } from '../../../../shared/ui/button';
import { useI18n } from '../../../../shared/i18n';
import {
  quickstartTemplates,
  quickstartTemplateCard,
  quickstartTemplateGrid,
  QuickstartTemplateContent,
} from '../templates';
import type { Builder } from './useBuilder';

export function BuilderStart({
  builder,
  composer,
  onStart,
}: {
  builder: Builder;
  composer: ReactNode;
  onStart: (action: () => void) => void;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const model = builder.state.config.model;
  const templates = quickstartTemplates(locale, typeof model === 'string' ? model : model.id);
  return (
    <section aria-labelledby="quickstart-start-title" className="min-h-0 min-w-0 flex-1 overflow-y-auto pb-12">
      <div className="mx-auto flex w-full max-w-[880px] flex-col items-center">
        <div className="w-full max-w-[728px]">{composer}</div>
        <div aria-label={zh ? '快速开始模板' : 'Quickstart templates'} className={`mt-10 ${quickstartTemplateGrid}`}>
          {templates.map(({ id, icon: Icon, config, description }) => (
            <Button
              key={id}
              aria-label={config.name}
              variant="ghost"
              className={quickstartTemplateCard}
              disabled={builder.busy || Boolean(builder.state.operation) || Boolean(builder.state.unconfirmedMessage)}
              onClick={() => onStart(() => void builder.startTemplate(config))}
            >
              <QuickstartTemplateContent icon={Icon} title={config.name} description={description} />
            </Button>
          ))}
        </div>
        <Button
          variant="link"
          className="mt-7 font-normal text-muted-foreground"
          disabled={builder.busy || Boolean(builder.state.operation) || Boolean(builder.state.unconfirmedMessage)}
          onClick={() =>
            onStart(
              () =>
                void builder.reply(
                  zh ? '从空白开始，先问我想实现什么。' : 'Start from scratch. Ask what I want to build first.',
                ),
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
