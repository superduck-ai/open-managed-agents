import { afterEach, describe } from 'bun:test';
import { registerManagedAgentsEnvironmentFailureTests } from './ManagedAgentsPage.environment-failures.suite';
import { registerManagedAgentsAgentsTests } from './ManagedAgentsPage.agents.suite';
import { registerManagedAgentsQuickstartTests } from './ManagedAgentsPage.quickstart.suite';
import { registerManagedAgentsResourceTests } from './ManagedAgentsPage.resources.suite';
import { resetManagedAgentsTestState } from './ManagedAgentsPage.test-utils';

afterEach(resetManagedAgentsTestState);

describe('ManagedAgentsPage', () => {
  registerManagedAgentsEnvironmentFailureTests();
  registerManagedAgentsQuickstartTests();
  registerManagedAgentsAgentsTests();
  registerManagedAgentsResourceTests();
});
