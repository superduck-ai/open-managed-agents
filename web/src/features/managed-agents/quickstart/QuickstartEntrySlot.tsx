import type { ReactNode } from 'react';

export function QuickstartEntrySlot({ children, accessory }: { children: ReactNode; accessory?: ReactNode }) {
  return (
    <div className="mx-auto w-full">
      {accessory ? <div className="mb-4 flex h-8 min-w-0 items-center">{accessory}</div> : null}
      <div className="h-[8.5rem]">{children}</div>
    </div>
  );
}
