import { useState } from 'react';
import {
  Blocks,
  Box,
  Sparkles,
  WandSparkles,
  Wrench,
  Workflow,
  FileText,
  FileType2,
  BookOpen,
  Folder,
  Image,
  ScanEye,
  Clapperboard,
  Film,
  Mic,
  Music2,
  Code2,
  GitFork,
  GitBranch,
  Landmark,
  PenLine,
  ChartNoAxesCombined,
  Search,
  Bot,
} from 'lucide-react';
import { Button } from '@/shared/ui/button';
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from '@/shared/ui/popover';

import { useI18n } from '@/shared/i18n';
import './skills.css';
export const skillIconOptions = [
  { key: 'blocks', label: '通用', Icon: Blocks, tone: 'neutral' },
  { key: 'box', label: '组件', Icon: Box, tone: 'blue' },
  { key: 'sparkles', label: '灵感', Icon: Sparkles, tone: 'violet' },
  { key: 'bot', label: '助手', Icon: Bot, tone: 'green' },
  { key: 'wrench', label: '工具', Icon: Wrench, tone: 'neutral' },
  { key: 'workflow', label: '流程', Icon: Workflow, tone: 'blue' },
  { key: 'file-text', label: '文档', Icon: FileText, tone: 'blue' },
  { key: 'file-type-2', label: 'PDF', Icon: FileType2, tone: 'red' },
  { key: 'book-open', label: '知识', Icon: BookOpen, tone: 'amber' },
  { key: 'folder', label: '资料', Icon: Folder, tone: 'amber' },
  { key: 'pen-line', label: '写作', Icon: PenLine, tone: 'blue' },
  { key: 'search', label: '检索', Icon: Search, tone: 'green' },
  { key: 'image', label: '图像', Icon: Image, tone: 'violet' },
  { key: 'wand-sparkles', label: '创作', Icon: WandSparkles, tone: 'violet' },
  { key: 'scan-eye', label: '识别', Icon: ScanEye, tone: 'violet' },
  { key: 'clapperboard', label: '视频', Icon: Clapperboard, tone: 'violet' },
  { key: 'film', label: '动画', Icon: Film, tone: 'violet' },
  { key: 'mic', label: '语音', Icon: Mic, tone: 'orange' },
  { key: 'music-2', label: '音乐', Icon: Music2, tone: 'orange' },
  {
    key: 'chart-no-axes-combined',
    label: '分析',
    Icon: ChartNoAxesCombined,
    tone: 'green',
  },
  { key: 'landmark', label: '金融', Icon: Landmark, tone: 'green' },
  { key: 'code-2', label: '代码', Icon: Code2, tone: 'blue' },
  { key: 'git-fork', label: '项目', Icon: GitFork, tone: 'green' },
  { key: 'git-branch', label: '编排', Icon: GitBranch, tone: 'blue' },
];
export function iconFor(key: string) {
  return skillIconOptions.find((option) => option.key === key) || skillIconOptions[0];
}

export function SkillIcon({ iconKey = 'blocks' }: { iconKey?: string }) {
  const { Icon, tone } = iconFor(iconKey);
  return (
    <span className="skill-glyph" data-tone={tone} data-icon-key={iconKey}>
      <Icon size={16} strokeWidth={1.65} aria-hidden />
    </span>
  );
}
export function SkillIconPicker({
  value,
  name,
  disabled = false,
  onChange,
}: {
  value: string;
  name: string;
  disabled?: boolean;
  onChange: (key: string) => void | Promise<void>;
}) {
  const { msg } = useI18n();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        if (!busy) {
          setOpen(next);
          setError('');
        }
      }}
    >
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            disabled={disabled}
            aria-label={msg('skills.icons.changeFor', 'Change icon for {name}', { name })}
          />
        }
      >
        <SkillIcon iconKey={value} />
      </PopoverTrigger>
      <PopoverContent className="skill-icon-popover" align="start">
        <PopoverTitle className="sr-only">{msg('skills.icons.choose', 'Choose icon')}</PopoverTitle>
        <div className="skill-icon-grid">
          {skillIconOptions.map(({ key, label, Icon }) => (
            <Button
              key={key}
              type="button"
              size="icon-sm"
              variant="ghost"
              className={value === key ? 'bg-accent' : ''}
              disabled={busy}
              aria-pressed={value === key}
              aria-label={msg(`skills.icons.${key}`, label)}
              title={msg(`skills.icons.${key}`, label)}
              onClick={async () => {
                setBusy(true);
                setError('');
                try {
                  await onChange(key);
                  setOpen(false);
                } catch (cause) {
                  setError(
                    cause instanceof Error
                      ? cause.message
                      : String((cause as { message?: string })?.message || msg('common.error', 'Error')),
                  );
                } finally {
                  setBusy(false);
                }
              }}
            >
              <Icon size={16} aria-hidden />
            </Button>
          ))}
        </div>
        {error && (
          <p role="alert" className="text-xs text-destructive">
            {error}
          </p>
        )}
      </PopoverContent>
    </Popover>
  );
}
