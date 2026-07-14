export const quickstartSteps = ['Create agent', 'Configure environment', 'Start session', 'Integrate'] as const;

export type QuickstartStepName = (typeof quickstartSteps)[number];
