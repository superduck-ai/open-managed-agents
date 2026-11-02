import type { ComponentType } from 'react';

export interface DshEvent {
  seq: number;
  time: number;
  type: string;
  data: Record<string, unknown>;
  surfaceOp?: unknown;
  sourceEventSeqs?: number[];
  ignorable?: true;
}

export interface DshHeader {
  version: 4;
  id: string;
  createdAt: number;
  parentSession?: string;
}

export interface DshAssembler {
  replaceWindow(entries: readonly { type: 'event'; event: DshEvent }[], hasMore: boolean): void;
  append(entry: { type: 'event'; event: DshEvent }): void;
  flush(): void;
  rebuildRegistry(): void;
  activateTarget(target: string): void;
  snapshot(target: string): unknown;
}

export interface DshConversationModule {
  ConversationNodeAssembler: new (
    events: { entries(): readonly unknown[]; fallbackEntry(): undefined },
    views: { entries(): readonly unknown[] },
  ) => DshAssembler;
  UiConversation: {
    prototype: {
      inspectSystemPrompt(...args: unknown[]): unknown;
      inspectRequestPrompt(...args: unknown[]): unknown;
    };
  };
}

export interface DshTrajectoryModule {
  apply(host: Record<string, unknown>): void;
}

export interface DshStore<T> {
  getSnapshot(): T;
  subscribe(listener: () => void): () => void;
  set(value: T): void;
}

export interface DshView {
  Component: ComponentType<Record<string, unknown>>;
  inject(id: string): {
    hooks: { duration: DshStore<boolean> };
    jsonStringWrapping: unknown;
    setActualDuration(value: boolean): void;
  };
}
