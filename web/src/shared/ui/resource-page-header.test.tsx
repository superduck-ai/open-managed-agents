import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { cleanup, render } from '@testing-library/react';
import { ResourcePageHeader } from './resource-page-header';

afterEach(cleanup);

test('stacks the primary action under the title on narrow screens', () => {
  const result = render(
    <ResourcePageHeader title="Webhooks" actions={<button type="button">Create webhook endpoint</button>} />,
  );

  const header = result.container.querySelector('header');
  expect(header?.className).toContain('flex-col');
  expect(header?.className).toContain('sm:flex-row');
  expect(header?.className).toContain('sm:justify-between');

  const action = header?.lastElementChild;
  expect(action?.className).toContain('shrink-0');
  expect(action?.textContent).toBe('Create webhook endpoint');
});
