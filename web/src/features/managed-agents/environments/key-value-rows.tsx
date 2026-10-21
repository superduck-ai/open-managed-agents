import { Plus, Trash2 } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Input } from '../../../shared/ui/input';
import { FieldError } from '../../../shared/ui/field';
import type { EnvironmentMetadataRow } from '../types';

export type KeyValueRowLabels = {
  key: (number: number) => string;
  value: (number: number) => string;
  remove: (key: string) => string;
  add: string;
  empty: string;
};

export function EnvironmentKeyValueRows({
  rows,
  onRowsChange,
  errors,
  readOnly,
  limit,
  labels,
}: {
  rows: EnvironmentMetadataRow[];
  onRowsChange: (rows: EnvironmentMetadataRow[]) => void;
  errors: Record<number, { key?: string; value?: string }>;
  readOnly?: boolean;
  limit: number;
  labels: KeyValueRowLabels;
}) {
  const { msg } = useI18n();
  const update = (index: number, field: 'key' | 'value', value: string) =>
    onRowsChange(rows.map((row, position) => (position === index ? { ...row, [field]: value } : row)));
  return (
    <>
      {rows.map((row, index) => (
        <div key={index}>
          <div className="grid grid-cols-[minmax(0,2fr)_minmax(0,3fr)_36px] gap-2">
            <Input
              aria-label={labels.key(index + 1)}
              autoFocus={!row.key && index === rows.length - 1}
              readOnly={readOnly}
              placeholder={msg('common.key', 'Key')}
              className="read-only:bg-muted/50"
              value={row.key}
              aria-invalid={Boolean(errors[index]?.key)}
              onChange={(event) => update(index, 'key', event.target.value)}
            />
            <Input
              aria-label={labels.value(index + 1)}
              readOnly={readOnly}
              placeholder={msg('common.value', 'Value')}
              className="read-only:bg-muted/50"
              value={row.value}
              aria-invalid={Boolean(errors[index]?.value)}
              onChange={(event) => update(index, 'value', event.target.value)}
            />
            {!readOnly ? (
              <Button
                variant="ghost"
                size="icon"
                aria-label={labels.remove(row.key || msg('environmentPage.emptyEntry', 'empty entry'))}
                onClick={() => onRowsChange(rows.filter((_, position) => index !== position))}
              >
                <Trash2 className="size-4" strokeWidth={1.5} />
              </Button>
            ) : null}
          </div>
          {errors[index] ? <FieldError>{errors[index].key || errors[index].value}</FieldError> : null}
        </div>
      ))}
      {readOnly && !rows.length ? <p className="text-sm text-muted-foreground">{labels.empty}</p> : null}
      {!readOnly ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          disabled={rows.length >= limit}
          onClick={() => onRowsChange([...rows, { key: '', value: '' }])}
        >
          <Plus className="size-4" strokeWidth={1.5} />
          {labels.add}
        </Button>
      ) : null}
    </>
  );
}
