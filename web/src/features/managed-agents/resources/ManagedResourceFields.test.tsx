import { afterEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { I18nProvider } from '../../../shared/i18n';
import { fireEvent, render, resetManagedAgentsTestState, screen } from '../ManagedAgentsPage.test-utils';
import { updateManagedEntityBody } from '../api';
import type { ManagedEntityFormValues, SessionResourceApiResponse } from '../types';
import { initialFormValues } from './model';
import { resourceFormValues } from './git-resource';
import { ManagedResourceFields } from './ManagedResourceFields';

afterEach(resetManagedAgentsTestState);

function mountResourceFields(resources: SessionResourceApiResponse[] = []) {
  let current = { ...initialFormValues('deployments'), ...resourceFormValues(resources) };
  function Sample() {
    const [values, setValues] = useState<ManagedEntityFormValues>(current);
    return (
      <I18nProvider initialLocale="en">
        <ManagedResourceFields
          values={values}
          workspaceId="default"
          editing
          memoryStores={[]}
          onChange={(next) => {
            current = next;
            setValues(next);
          }}
        />
      </I18nProvider>
    );
  }
  render(<Sample />);
  return () => current;
}

test('offers direct addition for an empty resource list without changing the update payload', () => {
  const values = mountResourceFields();
  expect(screen.queryByRole('button', { name: 'Replace resources' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Add resource' }));
  expect(screen.getByRole('menuitem', { name: 'Git repository' })).toBeTruthy();
  expect(values().resourcesChanged).toBe(false);
  expect(updateManagedEntityBody('deployments', { ...values(), name: 'Renamed' })).not.toHaveProperty('resources');
});

test('keeps unknown existing resources until replacement is explicitly chosen and can be cancelled', () => {
  const original = [{ type: 'future_resource', mount_path: '/workspace/context', extension: 'preserved' }];
  const values = mountResourceFields(original);
  expect(screen.queryByRole('button', { name: 'Add resource' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Replace resources' }));
  expect(values().resourcesChanged).toBe(true);
  expect(screen.getByText(/This replaces the entire resource list/)).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Keep existing resources' }));
  expect(values().originalResources).toEqual(original);
  expect(values().resourcesChanged).toBe(false);
  expect(updateManagedEntityBody('deployments', values())).not.toHaveProperty('resources');
  expect(screen.getByRole('button', { name: 'Replace resources' })).toBeTruthy();
});

test('adds a repository directly to an empty deployment and includes it only after editing', () => {
  const values = mountResourceFields();
  fireEvent.click(screen.getByRole('button', { name: 'Add resource' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'Git repository' }));
  expect(screen.queryByText(/This replaces the entire resource list/)).toBeNull();
  expect(screen.queryByRole('button', { name: 'Keep existing resources' })).toBeNull();
  fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://github.com/example/research' } });
  expect(updateManagedEntityBody('deployments', values())).toHaveProperty('resources', [
    { type: 'github_repository', url: 'https://github.com/example/research' },
  ]);
});
