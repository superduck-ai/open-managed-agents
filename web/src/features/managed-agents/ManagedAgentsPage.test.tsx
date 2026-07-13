import { afterEach, describe } from 'bun:test';
import { registerManagedAgentsAgentsTests } from './ManagedAgentsPage.agents.suite';
import { registerManagedAgentsQuickstartTests } from './ManagedAgentsPage.quickstart.suite';
import { registerManagedAgentsResourceTests } from './ManagedAgentsPage.resources.suite';
import { resetManagedAgentsTestState } from './ManagedAgentsPage.test-utils';

afterEach(resetManagedAgentsTestState);

describe('ManagedAgentsPage', () => {
  registerManagedAgentsQuickstartTests();
  registerManagedAgentsAgentsTests();
  registerManagedAgentsResourceTests();
});
