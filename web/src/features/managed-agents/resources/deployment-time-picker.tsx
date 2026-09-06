import { Clock3 } from 'lucide-react';
import { useState } from 'react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from '../../../shared/ui/popover';
import { DeploymentSelectField } from '../components/common';

const hours = Array.from({ length: 24 }, (_, hour) => ({
  id: String(hour).padStart(2, '0'),
  label: String(hour).padStart(2, '0'),
}));
const minutes = Array.from({ length: 60 }, (_, minute) => ({
  id: String(minute).padStart(2, '0'),
  label: String(minute).padStart(2, '0'),
}));

export function DeploymentTimePicker({
  id,
  value,
  onChange,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { msg, locale } = useI18n();
  const [open, setOpen] = useState(false);
  const validTime = /^([01]\d|2[0-3]):[0-5]\d$/.test(value);
  const [hour, minute] = (validTime ? value : '09:00').split(':');
  const displayTime = validTime
    ? new Intl.DateTimeFormat(locale, {
        hour: 'numeric',
        minute: '2-digit',
        timeZone: 'UTC',
      }).format(new Date(Date.UTC(2000, 0, 1, Number(hour), Number(minute))))
    : msg('managedAgents.deployments.schedule.chooseTime', 'Choose time');

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        id={id}
        render={<Button type="button" variant="outline" className="h-9 w-full justify-between font-normal" />}
      >
        <span className="tabular-nums">{displayTime}</span>
        <Clock3 className="size-4 text-muted-foreground" aria-hidden />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64 max-w-[calc(100vw-2rem)] gap-4 p-4">
        <PopoverTitle>{msg('managedAgents.deployments.schedule.chooseTime', 'Choose time')}</PopoverTitle>
        <div className="grid grid-cols-2 gap-3">
          <DeploymentSelectField
            label={msg('managedAgents.deployments.schedule.clockHour', 'Hour (24h)')}
            value={hour}
            placeholder=""
            options={hours}
            onChange={(nextHour) => nextHour && onChange(`${nextHour}:${minute}`)}
          />
          <DeploymentSelectField
            label={msg('managedAgents.deployments.schedule.clockMinute', 'Minute')}
            value={minute}
            placeholder=""
            options={minutes}
            onChange={(nextMinute) => nextMinute && onChange(`${hour}:${nextMinute}`)}
          />
        </div>
        <Button type="button" className="w-full" onClick={() => setOpen(false)}>
          {msg('common.done', 'Done')}
        </Button>
      </PopoverContent>
    </Popover>
  );
}
