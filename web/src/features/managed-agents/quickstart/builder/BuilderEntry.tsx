import { useState, type ReactNode } from 'react';
import { ArrowRight } from 'lucide-react';
import { useI18n } from '../../../../shared/i18n';
import { Button } from '../../../../shared/ui/button';
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
  AlertDialogAction,
} from '../../../../shared/ui/alert-dialog';
import type { Builder } from './useBuilder';
import { BuilderStart } from './BuilderStart';
import { BuilderComposer } from './BuilderComposer';

export function BuilderEntry({
  builder,
  startHeader,
  onEnter,
}: {
  builder: Builder;
  startHeader?: ReactNode;
  onEnter: () => void;
}) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const [pendingStart, setPendingStart] = useState<(() => void) | null>(null);
  const begin = (action: () => void) => {
    if (builder.state.initialized) {
      setPendingStart(() => action);
      return;
    }
    onEnter();
    action();
  };
  return (
    <>
      {startHeader}
      <BuilderStart
        builder={builder}
        onStart={begin}
        resume={
          builder.state.initialized && (
            <Button
              variant="ghost"
              className="h-8 max-w-full justify-start gap-2 font-normal"
              aria-label={zh ? '继续上次构建' : 'Continue previous build'}
              onClick={onEnter}
            >
              <span className="shrink-0">{zh ? '继续上次构建' : 'Continue previous build'}</span>
              <span className="min-w-0 truncate text-muted-foreground">{builder.state.config.name}</span>
              <ArrowRight className="shrink-0" />
            </Button>
          )
        }
        composer={<BuilderComposer builder={builder} entry onSend={(text) => begin(() => void builder.reply(text))} />}
      />
      <AlertDialog
        open={Boolean(pendingStart)}
        onOpenChange={(open) => {
          if (!open) setPendingStart(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{zh ? '开始新的构建？' : 'Start a new build?'}</AlertDialogTitle>
            <AlertDialogDescription>
              {builder.state.sessionID && !builder.state.testClosed
                ? zh
                  ? '当前本地构建记录将被替换。旧试跑会话和已创建资源会保留，可在“会话”页继续查看；正在运行的任务不会被停止。'
                  : 'The current local build history will be replaced. The previous test session will remain available on the Sessions page, along with saved resources. Running tasks will not be stopped.'
                : zh
                  ? '当前本地构建记录将被替换，已创建的智能体和环境仍会保留。'
                  : 'This replaces the current local build history. Saved agents and environments remain available.'}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{zh ? '取消' : 'Cancel'}</AlertDialogCancel>
            <AlertDialogAction
              disabled={builder.busy || Boolean(builder.state.operation) || Boolean(builder.state.unconfirmedMessage)}
              onClick={() => {
                if (!builder.restart()) return;
                onEnter();
                pendingStart?.();
                setPendingStart(null);
              }}
            >
              {zh ? '开始新的构建' : 'Start new build'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
