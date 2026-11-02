import { useId } from 'react';
import { useAvatarMotion, type AvatarState } from './avatarMotion';
import './builder-avatar.css';

export function BuilderAvatar({ state = 'still' }: { state?: AvatarState }) {
  const ref = useAvatarMotion(state);
  const id = useId();
  const rings = (layer: 'back' | 'front') => (
    <g fill="none" strokeWidth="5" strokeLinecap="round">
      {Array.from({ length: 3 }, (_, index) => (
        <path
          key={index}
          data-orbit={index}
          data-layer={layer}
          className="builder-avatar-orbit"
          stroke={`url(#${id}-${index})`}
        />
      ))}
    </g>
  );
  return (
    <svg
      ref={ref}
      aria-hidden="true"
      viewBox="-160 -160 320 320"
      className="builder-avatar -mt-1 size-8 shrink-0"
      data-state={state}
    >
      <defs>
        {Array.from({ length: 3 }, (_, index) => (
          <linearGradient key={index} id={`${id}-${index}`} x1="0" y1="0" x2="1" y2="1">
            <stop stopColor={`var(--${['violet', 'blue', 'cyan'][index]}-9)`} />
            <stop offset="1" stopColor={`var(--${['blue', 'cyan', 'violet'][index]}-9)`} />
          </linearGradient>
        ))}
      </defs>
      {rings('back')}
      <g className="builder-avatar-head">
        <path
          className="builder-avatar-body"
          d="M-28 -70 C-43 -81 -34 -100 -17 -103 C5 -110 27 -93 27 -70 C69 -59 92 -29 96 8 C97 17 96 23 97 26 C109 39 111 49 106 67 C96 98 58 109 0 109 C-58 109 -96 98 -106 67 C-111 49 -109 39 -97 26 C-96 23 -97 17 -96 8 C-93.12 -18.64 -80.39 -41.65 -58.18 -56.55 C-49.55 -62.35 -39.48 -66.92 -28 -70 Z"
        />
        <g className="builder-avatar-face">
          <g className="builder-avatar-gaze">
            <rect className="builder-avatar-eye" x="-49" y="-8" width="22" height="43" rx="11" />
            <rect className="builder-avatar-eye" x="27" y="-8" width="22" height="43" rx="11" />
          </g>
          <path
            className="builder-avatar-beak"
            d="M0 34 C-19 34 -26 44 -39 49 C-51 53 -59 55 -59 65 C-59 79 -37 84 0 84 C37 84 59 79 59 65 C59 55 51 53 39 49 C26 44 19 34 0 34 Z"
          />
        </g>
      </g>
      {rings('front')}
    </svg>
  );
}
