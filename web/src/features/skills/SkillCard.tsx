import type { ReactNode } from 'react';
import { SkillIcon } from './icons';

export function SkillCard({
  name,
  iconKey,
  description,
  metadata,
  footer,
  onOpen,
  actions,
}: {
  name: string;
  iconKey?: string;
  description?: string;
  metadata?: ReactNode;
  footer?: ReactNode;
  onOpen: () => void;
  actions?: ReactNode;
}) {
  return (
    <article className="skill-card-wrapper">
      <button type="button" className="skill-card" aria-label={name} aria-haspopup="dialog" onClick={onOpen}>
        <div className="skill-card-heading">
          <SkillIcon iconKey={iconKey} />
          <div className="min-w-0 flex-1">
            <h2 title={name}>{name}</h2>
            {metadata}
          </div>
        </div>
        {description !== undefined && <p className="skill-card-description">{description}</p>}
        {footer && <div className="skill-card-footer">{footer}</div>}
      </button>
      {actions && <div className="skill-card-actions">{actions}</div>}
    </article>
  );
}
