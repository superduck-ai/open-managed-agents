import { afterEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { resetTestDom } from '../../../test/setup';
import { ManagedResourceFields, managedResourceFieldsValid } from './ManagedResourceFields';
import { initialFormValues } from './model';
import { createManagedEntityBody } from '../api';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
afterEach(cleanup);

test('requires Git fields and submits the selected checkout through the shared resource form', async () => {
  resetTestDom('https://oma.duck.ai/');
  let submitted: unknown;
  function Form() {
    const [values, setValues] = useState(initialFormValues('sessions'));
    return (
      <form
        onSubmit={(event) => {
          event.preventDefault();
          submitted = createManagedEntityBody('sessions', values);
        }}
      >
        <ManagedResourceFields values={values} onChange={setValues} workspaceId="default" />
        <button type="submit" disabled={!managedResourceFieldsValid(values, false)}>
          Create session
        </button>
      </form>
    );
  }
  render(<Form />);
  fireEvent.click(screen.getByRole('button', { name: 'Add resource' }));
  fireEvent.click(screen.getByRole('menuitem', { name: 'GitHub repository' }));
  const submit = screen.getByRole('button', { name: 'Create session' });
  expect(submit.hasAttribute('disabled')).toBe(true);
  expect(screen.getByLabelText('Authorization token (optional for public repositories)').getAttribute('type')).toBe(
    'password',
  );
  fireEvent.change(screen.getByLabelText('URL'), { target: { value: 'https://github.com/owner/repo' } });
  expect(submit.hasAttribute('disabled')).toBe(false);
  expect(screen.getByLabelText('Authorization token (optional for public repositories)').hasAttribute('required')).toBe(
    false,
  );
  fireEvent.click(submit);
  expect(submitted).toMatchObject({ resources: [{ type: 'github_repository', url: 'https://github.com/owner/repo' }] });
  expect((submitted as { resources: object[] }).resources[0]).not.toHaveProperty('authorization_token');
  fireEvent.change(screen.getByLabelText('Authorization token (optional for public repositories)'), {
    target: { value: 'private-token' },
  });
  fireEvent.click(screen.getByRole('combobox', { name: 'Checkout (optional)' }));
  const branchOption = await screen.findByRole('option', { name: 'Branch' });
  fireEvent.pointerDown(branchOption);
  fireEvent.pointerUp(branchOption);
  fireEvent.click(branchOption);
  await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(true));
  fireEvent.change(screen.getByLabelText('Branch name'), { target: { value: 'main' } });
  fireEvent.click(submit);
  expect(submitted).toMatchObject({
    resources: [
      {
        type: 'github_repository',
        url: 'https://github.com/owner/repo',
        authorization_token: 'private-token',
        checkout: { type: 'branch', name: 'main' },
      },
    ],
  });
  fireEvent.click(screen.getByRole('button', { name: 'Remove GitHub repository 1' }));
  expect(screen.queryByLabelText('Authorization token (optional for public repositories)')).toBeNull();
});
