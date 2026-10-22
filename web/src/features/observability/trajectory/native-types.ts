import type { ComponentType } from 'react';

export interface NativeEvent {
  seq: number;
  time: number;
  type: string;
  data: Record<string, unknown>;
  surfaceOp?: unknown;
  sourceEventSeqs?: number[];
  ignorable?: true;
}

export interface NativeHeader {
  version: 4;
  id: string;
  createdAt: number;
  parentSession?: string;
}

export interface NativeAssembler {
  replaceWindow(entries: readonly { type: 'event'; event: NativeEvent }[], hasMore: boolean): void;
  append(entry: { type: 'event'; event: NativeEvent }): void;
  flush(): void;
  rebuildRegistry(): void;
  activateTarget(target: string): void;
  snapshot(target: string): unknown;
}

export interface NativeConversationModule {
  ConversationNodeAssembler: new (
    events: { entries(): readonly unknown[]; fallbackEntry(): undefined },
    views: { entries(): readonly unknown[] },
  ) => NativeAssembler;
  UiConversation: {
    prototype: {
      inspectSystemPrompt(...args: unknown[]): unknown;
      inspectRequestPrompt(...args: unknown[]): unknown;
    };
  };
}

export interface NativeTrajectoryModule {
  apply(host: Record<string, unknown>): void;
}

export interface NativeStore<T> {
  getSnapshot(): T;
  subscribe(listener: () => void): () => void;
  set(value: T): void;
}

export interface NativeView {
  Component: ComponentType<Record<string, unknown>>;
  inject(id: string): {
    hooks: { duration: NativeStore<boolean> };
    jsonStringWrapping: unknown;
    setActualDuration(value: boolean): void;
  };
}
