type Sleep = (ms: number) => Promise<void>;

export type TextStreamSmootherOptions = {
  onUpdate: (text: string) => void;
  signal?: AbortSignal;
  smoothingEnabled?: boolean;
  now?: () => number;
  sleep?: Sleep;
  isDocumentHidden?: () => boolean;
  isOutputVisible?: () => boolean;
  frameIntervalMs?: number;
  hiddenIntervalMs?: number;
  offscreenIntervalMs?: number;
  initialCharsPerSecond?: number;
  minCharsPerSecond?: number;
  minFinishCharsPerSecond?: number;
  maxCharsPerSecond?: number;
  maxFinishCharsPerSecond?: number;
  maxVisibleCharsPerStep?: number;
  maxHiddenCharsPerStep?: number;
  maxOffscreenCharsPerStep?: number;
};

type Arrival = {
  t: number;
  length: number;
};

export class TextStreamSmoother {
  private readonly onUpdate: (text: string) => void;
  private readonly signal?: AbortSignal;
  private readonly smoothingEnabled: boolean;
  private readonly now: () => number;
  private readonly sleep: Sleep;
  private readonly isDocumentHidden: () => boolean;
  private readonly isOutputVisible: () => boolean;
  private readonly frameIntervalMs: number;
  private readonly hiddenIntervalMs: number;
  private readonly offscreenIntervalMs: number;
  private readonly initialCharsPerSecond: number;
  private readonly minCharsPerSecond: number;
  private readonly minFinishCharsPerSecond: number;
  private readonly maxCharsPerSecond: number;
  private readonly maxFinishCharsPerSecond: number;
  private readonly maxVisibleCharsPerStep: number;
  private readonly maxHiddenCharsPerStep: number;
  private readonly maxOffscreenCharsPerStep: number;
  private readonly alpha = 0.99;
  private readonly abortHandler = () => this.abort();

  private target = '';
  private visible = '';
  private x = 0;
  private v: number;
  private start: number | null = null;
  private lastTick: number | null = null;
  private arrivals: Arrival[] = [{ t: -9999, length: 0 }];
  private modelDone = false;
  private forceDone = false;
  private running = false;
  private generation = 0;
  private idleResolvers: Array<() => void> = [];

  constructor(options: TextStreamSmootherOptions) {
    this.onUpdate = options.onUpdate;
    this.signal = options.signal;
    this.smoothingEnabled = options.smoothingEnabled ?? true;
    this.now = options.now ?? (() => performance.now());
    this.sleep = options.sleep ?? sleepForReveal;
    this.isDocumentHidden = options.isDocumentHidden ?? (() => Boolean(globalThis.document?.hidden));
    this.isOutputVisible = options.isOutputVisible ?? (() => true);
    this.frameIntervalMs = options.frameIntervalMs ?? 1000 / 60;
    this.hiddenIntervalMs = options.hiddenIntervalMs ?? 200;
    this.offscreenIntervalMs = options.offscreenIntervalMs ?? 100;
    this.initialCharsPerSecond = options.initialCharsPerSecond ?? 100;
    this.minCharsPerSecond = options.minCharsPerSecond ?? 70;
    this.minFinishCharsPerSecond = options.minFinishCharsPerSecond ?? 260;
    this.maxCharsPerSecond = options.maxCharsPerSecond ?? 540;
    this.maxFinishCharsPerSecond = options.maxFinishCharsPerSecond ?? 780;
    this.maxVisibleCharsPerStep = options.maxVisibleCharsPerStep ?? 14;
    this.maxHiddenCharsPerStep = options.maxHiddenCharsPerStep ?? 90;
    this.maxOffscreenCharsPerStep = options.maxOffscreenCharsPerStep ?? 70;
    this.v = this.initialCharsPerSecond;

    if (this.signal) {
      if (this.signal.aborted) {
        this.forceDone = true;
      } else {
        this.signal.addEventListener('abort', this.abortHandler, { once: true });
      }
    }
  }

  append(text: string) {
    if (!text) {
      return;
    }
    this.setTarget(`${this.target}${text}`);
  }

  setTarget(text: string) {
    if (text === this.target) {
      return;
    }
    this.ensureStart();
    this.target = text;
    this.recordArrival();

    if (!this.smoothingEnabled || this.forceDone) {
      this.flush();
      return;
    }

    if (this.x > this.target.length) {
      this.x = this.target.length;
      this.deliver();
    }
    this.startLoop();
  }

  async finish() {
    this.modelDone = true;
    if (!this.smoothingEnabled || this.forceDone) {
      this.flush();
      return;
    }
    if (this.target.length === 0) {
      this.resolveIdle();
      return;
    }
    this.startLoop();
    await this.waitForIdle();
  }

  reset() {
    this.generation += 1;
    this.target = '';
    this.visible = '';
    this.x = 0;
    this.v = this.initialCharsPerSecond;
    this.start = null;
    this.lastTick = null;
    this.arrivals = [{ t: -9999, length: 0 }];
    this.modelDone = false;
    this.forceDone = false;
    this.running = false;
    this.resolveIdle();
  }

  flush() {
    this.x = this.target.length;
    this.deliver();
    if (this.modelDone || this.forceDone || this.x >= this.target.length) {
      this.resolveIdle();
    }
  }

  abort() {
    this.forceDone = true;
    this.flush();
    this.generation += 1;
    this.running = false;
  }

  dispose() {
    this.signal?.removeEventListener('abort', this.abortHandler);
    this.forceDone = true;
    this.generation += 1;
    this.running = false;
    this.resolveIdle();
  }

  private startLoop() {
    if (this.running || this.signal?.aborted) {
      return;
    }
    this.running = true;
    const generation = this.generation;
    void this.task(generation);
  }

  private async task(generation: number) {
    while (this.generation === generation && !this.forceDone && !this.signal?.aborted) {
      const hidden = this.isDocumentHidden();
      const outputVisible = !hidden && this.isOutputVisible();
      const intervalMs = hidden
        ? this.hiddenIntervalMs
        : outputVisible
          ? this.frameIntervalMs
          : this.offscreenIntervalMs;
      const changed = this.advance({
        intervalMs,
        maxCharsPerStep: hidden
          ? this.maxHiddenCharsPerStep
          : outputVisible
            ? this.maxVisibleCharsPerStep
            : this.maxOffscreenCharsPerStep,
      });
      if (changed) {
        this.deliver();
      }
      if (this.modelDone && this.x >= this.target.length) {
        break;
      }
      await this.sleep(intervalMs);
    }

    if (this.generation === generation) {
      this.running = false;
      if (this.modelDone || this.forceDone || this.x >= this.target.length) {
        this.resolveIdle();
      }
    }
  }

  private advance({ intervalMs, maxCharsPerStep }: { intervalMs: number; maxCharsPerStep: number }) {
    if (this.target.length <= this.x) {
      return false;
    }

    const now = this.now();
    const elapsedMs = this.lastTick === null ? this.frameIntervalMs : Math.max(1, now - this.lastTick);
    const cappedElapsedMs = Math.min(elapsedMs, Math.max(this.frameIntervalMs, intervalMs));
    const dt = cappedElapsedMs / 1000;
    this.lastTick = now;

    const arrivalRate = Math.min(this.estimatedArrivalRate(now), this.maxCharsPerSecond);
    if (arrivalRate > 0) {
      this.v = this.alpha * this.v + (1 - this.alpha) * arrivalRate;
    }

    const targetRate = this.modelDone
      ? Math.min(this.maxFinishCharsPerSecond, Math.max(this.v * 1.35, this.minFinishCharsPerSecond))
      : Math.min(this.maxCharsPerSecond, Math.max(this.v * 1.05, this.minCharsPerSecond));
    const step = Math.min(maxCharsPerStep, Math.max(1, targetRate * dt));
    const nextX = Math.min(this.target.length, this.x + step);
    if (nextX <= this.x) {
      return false;
    }
    this.x = nextX;
    return true;
  }

  private estimatedArrivalRate(nowMs: number) {
    if (this.arrivals.length < 2 || this.start === null) {
      return 0;
    }
    const nowSeconds = (nowMs - this.start) / 1000;
    const cutoff = 0.9 * nowSeconds - 0.3;
    const usable = this.arrivals.filter((arrival) => arrival.t >= 0 && arrival.t < cutoff);
    const history = usable.length >= 2 ? usable : this.arrivals.filter((arrival) => arrival.t >= 0);
    if (history.length < 2) {
      return 0;
    }
    const first = history[0];
    const last = history[history.length - 1];
    const dt = Math.max(0.001, last.t - first.t);
    return Math.max(0, (last.length - first.length) / dt);
  }

  private deliver() {
    const nextVisible = this.target.slice(0, Math.ceil(this.x));
    if (nextVisible !== this.visible) {
      this.visible = nextVisible;
      this.onUpdate(nextVisible);
    }
  }

  private ensureStart() {
    if (this.start === null) {
      this.start = this.now();
      this.lastTick = this.start;
    }
  }

  private recordArrival() {
    if (this.start === null) {
      return;
    }
    this.arrivals.push({
      t: (this.now() - this.start) / 1000,
      length: this.target.length,
    });
  }

  private waitForIdle() {
    if (!this.running && (this.modelDone || this.forceDone || this.x >= this.target.length)) {
      return Promise.resolve();
    }
    return new Promise<void>((resolve) => {
      this.idleResolvers.push(resolve);
    });
  }

  private resolveIdle() {
    const resolvers = this.idleResolvers;
    this.idleResolvers = [];
    resolvers.forEach((resolve) => resolve());
  }
}

function sleepForReveal(ms: number) {
  if (ms <= 1000 / 30 && !globalThis.document?.hidden && typeof globalThis.requestAnimationFrame === 'function') {
    return new Promise<void>((resolve) => {
      globalThis.requestAnimationFrame(() => resolve());
    });
  }
  return new Promise<void>((resolve) => globalThis.setTimeout(resolve, ms));
}
