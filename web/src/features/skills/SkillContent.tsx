import { ChevronDown, FileText, Folder } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/shared/ui/collapsible';
import type { SkillContent as SkillContentData } from './api';
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

function FileTree({ nodes }: { nodes: Map<string, FileNode> }) {
  return (
    <ul className="space-y-1">
      {[...nodes.values()]
        .sort((a, b) => Number(b.directory) - Number(a.directory) || a.name.localeCompare(b.name))
        .map((node) => (
          <li key={node.path}>
            <div className="flex min-w-0 items-center gap-2 py-0.5 text-xs" title={node.path}>
              {node.directory ? (
                <Folder className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
              ) : (
                <FileText className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
              )}
              <span className="min-w-0 break-all font-mono">{node.name}</span>
            </div>
            {node.children.size > 0 && (
              <div className="pl-4 pt-1">
                <FileTree nodes={node.children} />
              </div>
            )}
          </li>
        ))}
    </ul>
  );
}

export function SkillContent({ content }: { content: SkillContentData }) {
  const { msg } = useI18n();
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
          <nav aria-label={msg('skills.content.files', 'Files')} className="max-h-60 overflow-y-auto">
            <FileTree nodes={skillFileTree(content.files)} />
          </nav>
        </CollapsibleContent>
      </Collapsible>
      <section className="min-w-0 border-t-[0.5px] border-border pt-4" aria-label="SKILL.md">
        <h2 className="mb-4 text-sm font-medium">SKILL.md</h2>
        <pre className="skill-source">
          <code className="language-markdown">{content.skill_md}</code>
        </pre>
      </section>
    </div>
  );
}
