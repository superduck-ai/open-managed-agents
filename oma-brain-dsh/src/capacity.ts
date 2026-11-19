import { CapacityExhaustedError } from "./claim.ts";

export const IDLE_RETIREMENT_MS = 60_000;

export interface CapacityMember {
  readonly id: string;
  readonly busy: boolean;
  readonly lastIdle: number;
  readonly initializing?: boolean;
}

export function idleCandidate(member: CapacityMember): boolean {
  return !member.busy && !member.initializing;
}

export function idleExpired(member: CapacityMember, now: number): boolean {
  return idleCandidate(member) && now - member.lastIdle > IDLE_RETIREMENT_MS;
}

export class SessionCapacity<T extends CapacityMember> {
  private reserved = 0;

  constructor(
    private readonly maxSessions: number,
    private readonly sessions: ReadonlyMap<string, T>,
    private readonly retiring: ReadonlyMap<string, unknown>,
    private readonly evict: (session: T) => void,
  ) {}

  available(): number {
    return this.maxSessions - [...this.sessions.values()].filter((session) => session.busy).length;
  }

  reserve(): () => void {
    this.reserved++;
    try {
      while (this.sessions.size - this.retiring.size + this.reserved > this.maxSessions) {
        const idle = [...this.sessions.values()]
          .filter((session) => idleCandidate(session) && !this.retiring.has(session.id))
          .sort((a, b) => a.lastIdle - b.lastIdle)[0];
        if (!idle) throw new CapacityExhaustedError();
        this.evict(idle);
      }
    } catch (error) {
      this.reserved--;
      throw error;
    }
    let held = true;
    return () => {
      if (held) this.reserved--;
      held = false;
    };
  }
}
