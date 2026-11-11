import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChevronDown, FileCode2, FileText, Folder } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/shared/ui/collapsible';
import type { SkillFilePreview, SkillContent as SkillContentData } from './api';
import { SkillCodeBlock, skillCodeLanguage } from './SkillCodeBlock';
import './skills.css';

type FileNode = { name: string; path: string; children: Map<string, FileNode>; directory: boolean };

function skillFileTree(paths: string[]) {
  const root = new Map<string, FileNode>();
  for (const path of paths) {
    const parts = path.split('/').filter(Boolean);
    let level = root;
    parts.forEach((name, index) => {
      if (!level.has(name))
        level.set(name, { name, path: parts.slice(0, index + 1).join('/'), children: new Map(), directory: false });
      const node = level.get(name)!;
      node.directory ||= index < parts.length - 1 || path.endsWith('/');
      level = node.children;
    });
  }
  return root;
}

function FileTree({
  nodes,
  selected,
  onSelect,
  defaultOpen = false,
}: {
  defaultOpen?: boolean;
  nodes: Map<string, FileNode>;
  selected: string;
  onSelect: (path: string) => void;
}) {
  return (
    <ul className="space-y-1">
      {[...nodes.values()]
        .sort((a, b) => Number(b.directory) - Number(a.directory) || a.name.localeCompare(b.name))
        .map((node) => (
          <li key={node.path}>
            {node.directory ? (
              <Collapsible defaultOpen={defaultOpen}>
                <CollapsibleTrigger
                  render={
                    <Button
                      variant="ghost"
                      className="group h-auto w-full justify-start gap-2 px-2 py-1 text-left text-xs font-normal focus-visible:ring-0 focus-visible:border-transparent focus-visible:bg-accent"
                    />
                  }
                  title={node.path}
                >
                  <ChevronDown
                    className="size-3.5 shrink-0 -rotate-90 text-muted-foreground transition-transform group-aria-expanded:rotate-0"
                    aria-hidden
                  />
                  <Folder className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                  <span className="min-w-0 break-all whitespace-normal font-mono">{node.name}</span>
                </CollapsibleTrigger>
                <CollapsibleContent>
                  <div className="pl-4 pt-1">
                    <FileTree nodes={node.children} selected={selected} onSelect={onSelect} />
                  </div>
                </CollapsibleContent>
              </Collapsible>
            ) : (
              <Button
                variant="ghost"
                className="h-auto w-full justify-start gap-2 px-2 py-1 text-left text-xs font-normal data-[selected=true]:bg-accent focus-visible:ring-0 focus-visible:border-transparent focus-visible:bg-accent"
                title={node.path}
                aria-current={selected === node.path ? 'true' : undefined}
                data-selected={selected === node.path}
                onClick={() => onSelect(node.path)}
              >
                <span className="size-3.5 shrink-0" aria-hidden />
                <FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                <span className="min-w-0 break-all whitespace-normal font-mono">{node.name}</span>
              </Button>
            )}
          </li>
        ))}
    </ul>
  );
}

export function SkillContent({
  content,
  contentKey,
  loadFile,
}: {
  content: SkillContentData;
  contentKey: string;
  loadFile: (path: string, signal: AbortSignal) => Promise<SkillFilePreview>;
}) {
  const { msg } = useI18n();
  const defaultPath = content.files.find((path) => path.split('/').length === 2 && path.endsWith('/SKILL.md')) || '';
  const [selected, setSelected] = useState(defaultPath);
  const query = useQuery({
    queryKey: ['skillFilePreview', contentKey, selected],
    queryFn: ({ signal }) => loadFile(selected, signal),
    enabled: selected !== defaultPath,
    retry: false,
  });
  const source = selected === defaultPath ? content.skill_md : query.data?.content;

  return (
    <div className="min-w-0 space-y-5">
      <Collapsible defaultOpen>
        <CollapsibleTrigger render={<Button variant="ghost" size="sm" className="group -ml-2 mb-2" />}>
          <ChevronDown className="size-3.5 transition-transform group-aria-expanded:rotate-0 -rotate-90" aria-hidden />
          {msg('skills.content.files', 'Files')}
          <span className="text-xs text-muted-foreground">
            {content.files.filter((path) => !path.endsWith('/')).length}
          </span>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <nav aria-label={msg('skills.content.files', 'Files')} className="subtle-scrollbar max-h-60 overflow-y-auto">
            <FileTree nodes={skillFileTree(content.files)} selected={selected} onSelect={setSelected} defaultOpen />
          </nav>
        </CollapsibleContent>
      </Collapsible>
      <section className="skill-preview" aria-label={selected}>
        <header className="flex min-w-0 items-center gap-2 border-b-[0.5px] border-border px-4 py-2.5">
          <FileCode2 className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <h2 className="min-w-0 flex-1 truncate font-mono text-xs font-medium" title={selected}>
            {selected.replace(/^[^/]+\//, '') || 'SKILL.md'}
          </h2>
          <span className="shrink-0 text-xs text-muted-foreground">{skillCodeLanguage(selected).label}</span>
        </header>
        {selected !== defaultPath && query.isPending ? (
          <p role="status" className="px-4 py-5 text-sm text-muted-foreground">
            {msg('common.loading', 'Loading...')}
          </p>
        ) : selected !== defaultPath && query.isError ? (
          <div role="alert" className="space-y-2 p-4 text-sm text-muted-foreground">
            <p>{msg('skills.file.error', 'File preview could not be loaded.')}</p>
            <Button variant="outline" size="sm" onClick={() => void query.refetch()}>
              {msg('common.retry', 'Retry')}
            </Button>
          </div>
        ) : selected !== defaultPath && !query.data?.previewable ? (
          <p className="px-4 py-5 text-sm text-muted-foreground">
            {query.data?.reason === 'too_large'
              ? msg('skills.file.tooLarge', 'Cannot preview files larger than 256 KiB.')
              : msg('skills.file.unsupported', 'Cannot preview this file.')}
          </p>
        ) : (
          <SkillCodeBlock source={source ?? ''} path={selected} />
        )}
      </section>
    </div>
  );
}
