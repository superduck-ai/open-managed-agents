import type {
  NativeAssembler,
  NativeConversationModule,
  NativeEvent,
  NativeTrajectoryModule,
  NativeView,
} from './native-types';

type Dictionary = Record<string, string>;
type Translator = (key: string, variables?: Record<string, unknown>) => string;

export interface Projection {
  engine: NativeAssembler;
  view: NativeView;
  translate: Translator;
  append(events: NativeEvent[]): unknown;
}

export function createProjection(
  native: NativeConversationModule,
  trajectory: NativeTrajectoryModule,
  language: string,
): Projection {
  const definitions: unknown[] = [];
  const views: unknown[] = [];
  const dictionaries = new Map<string, Dictionary>();
  let view: NativeView | undefined;
  const translate: Translator = (key, variables = {}) => {
    const text = dictionaries.get(language.startsWith('zh') ? 'zh' : 'en')?.[key] ?? key;
    return text.replace(/\{([^}]+)\}/g, (_, name: string) => String(variables[name] ?? ''));
  };
  const engine = new native.ConversationNodeAssembler(
    { entries: () => definitions, fallbackEntry: () => undefined },
    { entries: () => views },
  );
  const target = { getSnapshot: () => engine.snapshot('trajectory'), subscribe: () => () => {} };
  trajectory.apply({
    effect: (run: () => unknown) => run(),
    locale: {
      register: (_namespace: string, values: Record<string, Dictionary>) => {
        for (const [key, value] of Object.entries(values)) dictionaries.set(key, value);
      },
      bind: () => translate,
    },
    uiConversation: {
      events: {
        register: (definition: unknown) => {
          definitions.push(definition);
        },
      },
      views: {
        register: (definition: unknown) => {
          views.push(definition);
        },
      },
      binding: () => ({ target: () => target }),
      inspectSystemPrompt: (...args: unknown[]) => native.UiConversation.prototype.inspectSystemPrompt(...args),
      inspectRequestPrompt: (...args: unknown[]) => native.UiConversation.prototype.inspectRequestPrompt(...args),
    },
    uiSession: { provide: () => {} },
    sessions: { binding: () => ({ session: { loadOlder: async () => {} } }) },
    slots: {
      inject: (_name: string, install: () => unknown) => install(),
      register: (entry: Omit<NativeView, 'Component'>, Component: NativeView['Component']) => {
        view = { ...entry, Component };
      },
    },
  });
  if (!view) throw new Error('Harness Trajectory did not register its view');
  engine.rebuildRegistry();
  engine.replaceWindow([], false);
  engine.activateTarget('trajectory');
  engine.flush();
  let lastSeq = -1;
  return {
    engine,
    view,
    translate,
    append(events) {
      for (const event of events) {
        if (event.seq <= lastSeq) continue;
        engine.append({ type: 'event', event });
        lastSeq = event.seq;
      }
      engine.flush();
      return engine.snapshot('trajectory');
    },
  };
}
