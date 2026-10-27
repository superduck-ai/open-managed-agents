import { ShieldAlert } from 'lucide-react';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from './empty';

export function AccessDenied({ title, description }: { title: string; description: string }) {
  return (
    <Empty className="min-h-[320px]">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <ShieldAlert aria-hidden />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}
