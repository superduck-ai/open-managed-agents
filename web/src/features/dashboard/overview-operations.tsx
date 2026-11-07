import { ArrowUpRight } from 'lucide-react';
import { productIcons } from '@/shared/icons/product-icons';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import { useI18n } from '@/shared/i18n';
import type { OverviewAgent } from './overview-api';

type OperationsProps = {
  agents: OverviewAgent[];
  period: string;
  workspacePath: string;
  selectedAgent: string;
  onSelectAgent: (id: string) => void;
};

export function OverviewOperations({ agents, period, workspacePath, selectedAgent, onSelectAgent }: OperationsProps) {
  const { locale } = useI18n();
  const zh = locale === 'zh-CN';
  const text = (chinese: string, english: string) => (zh ? chinese : english);
  return (
    <section aria-label={text('智能体运行情况', 'Agent activity')} className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-sm font-medium">{text('智能体运行情况', 'Agent activity')}</h2>
        <a href={`${workspacePath}/agents`} className="text-xs text-muted-foreground hover:text-foreground">
          {text('全部智能体', 'All agents')} <ArrowUpRight className="inline size-3.5" aria-hidden />
        </a>
      </div>
      <Table aria-label={zh ? `过去 ${period} 天智能体运行情况` : `Agent activity in the past ${period} days`}>
        <TableHeader>
          <TableRow>
            <TableHead className="pl-4">{text('智能体', 'Agent')}</TableHead>
            <TableHead className="text-right">{text('会话数', 'Sessions')}</TableHead>
            <TableHead className="text-right">{text('失败数', 'Failed')}</TableHead>
            <TableHead className="hidden pr-4 text-right sm:table-cell">{text('最近会话', 'Latest session')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {agents.map((item) => (
            <TableRow key={item.agent_id}>
              <TableCell className="max-w-40 pl-4 py-3 sm:max-w-80">
                <button
                  type="button"
                  title={item.name}
                  aria-label={zh ? `查看${item.name}趋势` : `View trend for ${item.name}`}
                  aria-pressed={selectedAgent === item.agent_id}
                  onClick={() => onSelectAgent(item.agent_id)}
                  className="flex max-w-full items-center gap-2 rounded text-sm outline-none hover:underline focus-visible:ring-[0.5px] focus-visible:ring-foreground"
                >
                  <productIcons.agents className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                  <span className="truncate">{item.name}</span>
                </button>
              </TableCell>
              <TableCell className="text-right tabular-nums">{item.session_count.toLocaleString()}</TableCell>
              <TableCell
                className={`text-right tabular-nums ${item.failed_session_count > 0 ? 'text-destructive' : 'text-muted-foreground'}`}
              >
                {item.failed_session_count.toLocaleString()}
              </TableCell>
              <TableCell className="hidden pr-4 text-right text-xs text-muted-foreground sm:table-cell">
                <time dateTime={item.last_run_at}>
                  {new Date(item.last_run_at).toLocaleString(locale, {
                    month: 'numeric',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit',
                  })}
                </time>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </section>
  );
}
