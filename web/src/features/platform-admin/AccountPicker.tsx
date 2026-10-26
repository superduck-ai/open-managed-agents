import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Button } from '../../shared/ui/button';
import { Checkbox } from '../../shared/ui/checkbox';
import { Input } from '../../shared/ui/input';
import { usePlatformAdmin } from './context';
import type { Page } from './types';

export type RegisteredAccount = { uuid: string; name: string; email: string };

export function AccountPicker({
  selected,
  onChange,
  disabled = false,
  limit = 20,
}: {
  selected: RegisteredAccount[];
  onChange: (accounts: RegisteredAccount[]) => void;
  disabled?: boolean;
  limit?: number;
}) {
  const { msg } = useI18n();

  const { client } = usePlatformAdmin();
  const [input, setInput] = useState('');
  const [search, setSearch] = useState('');
  const [cursor, setCursor] = useState('');
  const query = useQuery({
    queryKey: ['platform-admin', 'accounts', search, cursor],
    queryFn: ({ signal }) =>
      client.request<Page<RegisteredAccount>>(`/accounts?${new URLSearchParams({ query: search, cursor })}`, {
        signal,
      }),
    enabled: search.length >= 2,
  });
  function select(account: RegisteredAccount, checked: boolean) {
    onChange(checked ? [...selected, account] : selected.filter((item) => item.uuid !== account.uuid));
  }
  return (
    <div className="space-y-3">
      <div className="flex gap-2">
        <Input
          aria-label={msg('platformAdmin.searchRegisteredAccounts', 'Search registered accounts')}
          placeholder={msg('platformAdmin.emailOrNameAtLeastTwoCharacters', 'Email or name, at least two characters')}
          maxLength={200}
          value={input}
          disabled={disabled}
          onChange={(event) => setInput(event.target.value)}
        />
        <Button
          type="button"
          variant="outline"
          disabled={disabled || input.trim().length < 2 || query.isFetching}
          onClick={() => {
            setSearch(input.trim());
            setCursor('');
          }}
        >
          {msg('platformAdmin.find', 'Find')}
        </Button>
      </div>
      {selected.length > 0 && (
        <ul aria-label={msg('platformAdmin.selectedAdministrators', 'Selected administrators')} className="space-y-1">
          {selected.map((account) => (
            <li key={account.uuid} className="flex items-center justify-between gap-2 rounded-md bg-muted px-3 py-1">
              <span className="min-w-0" title={[account.name, account.email].filter(Boolean).join(' ')}>
                {account.name && <span className="block truncate">{account.name}</span>}
                <span className="block truncate text-muted-foreground">{account.email}</span>
              </span>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                disabled={disabled}
                aria-label={msg('platformAdmin.removeEmail', 'Remove {email}', { email: account.email })}
                onClick={() => select(account, false)}
              >
                {msg('platformAdmin.remove', 'Remove')}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {query.isFetching && <p role="status">{msg('platformAdmin.findingAccounts', 'Finding accounts…')}</p>}
      {query.isError && (
        <p role="alert">
          {msg(
            'platformAdmin.accountLookupFailedOrWasRateLimitedPleaseTry',
            'Account lookup failed or was rate limited. Please try again later.',
          )}
          <Button type="button" variant="link" onClick={() => void query.refetch()}>
            {msg('platformAdmin.retry', 'Retry')}
          </Button>
        </p>
      )}
      {query.data && !query.isError && (
        <div className="max-h-48 overflow-auto rounded-md border-[0.5px] p-3">
          {!query.data.data.length && (
            <p className="text-muted-foreground">
              {msg(
                'platformAdmin.noMatchingAccountsInitialAdministratorsMustRegisterFirst',
                'No matching accounts. Initial administrators must register first.',
              )}
            </p>
          )}
          {query.data.data.map((account) => {
            const checked = selected.some((item) => item.uuid === account.uuid);
            return (
              <div key={account.uuid} className="flex items-center gap-3 py-2">
                <Checkbox
                  aria-label={msg('platformAdmin.selectEmail', 'Select {email}', { email: account.email })}
                  checked={checked}
                  disabled={disabled || (!checked && selected.length >= limit)}
                  onCheckedChange={(value) => select(account, value)}
                />
                <span className="min-w-0 break-all">
                  <span className="block">{account.name || msg('platformAdmin.noNameSet', 'No name set')}</span>
                  <span className="block text-muted-foreground">{account.email}</span>
                </span>
              </div>
            );
          })}
          {query.data.next_cursor && (
            <Button
              type="button"
              variant="outline"
              disabled={disabled || query.isFetching}
              onClick={() => setCursor(query.data.next_cursor)}
            >
              {msg('platformAdmin.nextAccounts', 'Next accounts')}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
