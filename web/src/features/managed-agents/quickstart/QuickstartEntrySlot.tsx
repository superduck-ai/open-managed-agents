import type { ReactNode } from 'react';

export function QuickstartEntrySlot({ children, accessory }: { children: ReactNode; accessory?: ReactNode }) {
  return (
    <div className="mx-auto w-full">
      <div className="mb-6 flex h-8 min-w-0 items-center">{accessory}</div>
      <div className="h-32">{children}</div>
    </div>
  );
}
