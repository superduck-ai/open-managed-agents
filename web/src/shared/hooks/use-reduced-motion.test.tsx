import { afterEach, expect, test } from 'bun:test';
import { act, cleanup, render, screen } from '@testing-library/react';
import { useReducedMotion } from './use-reduced-motion';

const originalMatchMedia = Object.getOwnPropertyDescriptor(window, 'matchMedia');

afterEach(() => {
  cleanup();
  if (originalMatchMedia) {
    Object.defineProperty(window, 'matchMedia', originalMatchMedia);
  } else {
    Reflect.deleteProperty(window, 'matchMedia');
  }
});

function MotionPreference() {
  return <output>{useReducedMotion() ? 'Reduced motion' : 'Full motion'}</output>;
}

test('renders safely when the media-query API is unavailable', () => {
  Object.defineProperty(window, 'matchMedia', { value: undefined, configurable: true });
  render(<MotionPreference />);
  expect(screen.getByText('Full motion')).toBeTruthy();
});

test('responds to reduced-motion preference changes while mounted', () => {
  let reduced = true;
  const media = new EventTarget();
  Object.defineProperty(media, 'matches', { get: () => reduced });
  Object.defineProperty(window, 'matchMedia', { value: () => media, configurable: true });
  render(<MotionPreference />);
  expect(screen.getByText('Reduced motion')).toBeTruthy();
  act(() => {
    reduced = false;
    media.dispatchEvent(new Event('change'));
  });
  expect(screen.getByText('Full motion')).toBeTruthy();
  act(() => {
    reduced = true;
    media.dispatchEvent(new Event('change'));
  });
  expect(screen.getByText('Reduced motion')).toBeTruthy();
});
