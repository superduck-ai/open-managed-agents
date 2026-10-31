import { useEffect, useRef } from 'react';
import { useReducedMotion } from '@/shared/hooks/use-reduced-motion';

export type AvatarState = 'still' | 'idle' | 'thinking' | 'speaking';

const TAU = Math.PI * 2;
const clamp = (value: number) => Math.max(0, Math.min(1, value));
const smooth = (value: number) => {
  const t = clamp(value);
  return t * t * (3 - 2 * t);
};

function orbitPath(index: number, time: number, front: boolean) {
  const angle = (index / 3) * Math.PI - 0.5;
  const radius = 132;
  const flatten = 0.32;
  const phase = time * TAU + index * 2;
  const sweep = 0.38 * TAU;
  let path = '';
  let connected = false;
  for (let step = 0; step <= 64; step++) {
    const theta = phase + (step / 64) * sweep;
    const x = radius * (Math.cos(theta) * Math.cos(angle) - Math.sin(theta) * Math.sin(angle) * flatten);
    const y = radius * (Math.cos(theta) * Math.sin(angle) + Math.sin(theta) * Math.cos(angle) * flatten);
    if (Math.sin(theta) >= 0 === front) {
      path += `${connected ? 'L' : 'M'}${x.toFixed(2)} ${y.toFixed(2)}`;
      connected = true;
    } else connected = false;
  }
  return path;
}

function animateAvatar(svg: SVGSVGElement, getState: () => AvatarState) {
  const head = svg.querySelector<SVGGElement>('.builder-avatar-head')!;
  const gaze = svg.querySelector<SVGGElement>('.builder-avatar-gaze')!;
  const arcs = Array.from(svg.querySelectorAll<SVGPathElement>('[data-orbit]'));
  let previous = performance.now();
  let time = 0;
  let thinking = 0;
  let frame = 0;
  let spin = 0;
  const draw = (now: number) => {
    if (document.hidden) {
      previous = now;
      frame = requestAnimationFrame(draw);
      return;
    }
    const state = getState();
    thinking += ((state === 'thinking' ? 1 : 0) - thinking) * (1 - Math.exp(-(now - previous) / 180));
    const elapsed = Math.min((now - previous) / 1000, 0.05);
    previous = now;
    time += elapsed;
    const orbit = smooth((time - 2) / 0.8);
    spin += elapsed * 0.32;
    const lookCycle = time % 6;
    const look = smooth(lookCycle / 0.5) * (1 - smooth((lookCycle - 2.3) / 0.7));
    const yaw = look * (3 + thinking * 5);
    gaze.setAttribute('transform', `translate(${yaw} ${-look * (1 + thinking * 4)})`);
    head.setAttribute(
      'transform',
      `rotate(${-thinking * look * 3}) translate(0 ${thinking * Math.sin(time * 1.8) * 2})`,
    );
    arcs.forEach((arc) => {
      const index = Number(arc.dataset.orbit);
      arc.setAttribute('d', orbitPath(index, spin, arc.dataset.layer === 'front'));
      arc.style.opacity = String(thinking * orbit * 0.65);
    });
    frame = requestAnimationFrame(draw);
  };
  frame = requestAnimationFrame(draw);
  return () => {
    cancelAnimationFrame(frame);
    head.removeAttribute('transform');
    gaze.removeAttribute('transform');
    arcs.forEach((arc) => {
      arc.style.opacity = '0';
    });
  };
}

export function useAvatarMotion(state: AvatarState) {
  const ref = useRef<SVGSVGElement>(null);
  const currentState = useRef(state);
  useEffect(() => {
    currentState.current = state;
  }, [state]);
  const reducedMotion = useReducedMotion();
  const active = state === 'thinking' || state === 'speaking';
  useEffect(() => {
    const svg = ref.current;
    if (!svg || !active || reducedMotion) return;
    return animateAvatar(svg, () => currentState.current);
  }, [active, reducedMotion]);
  return ref;
}
