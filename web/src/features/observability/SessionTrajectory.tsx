import { ObservabilityPage } from './ObservabilityPage';

export function SessionTrajectory({ sessionId }: { sessionId: string }) {
  return <ObservabilityPage scope={{ kind: 'session', sessionId }} />;
}
