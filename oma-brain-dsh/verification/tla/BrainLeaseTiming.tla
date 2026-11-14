-------------------------- MODULE BrainLeaseTiming --------------------------
EXTENDS Integers

CONSTANTS LeaseTTL, Grace, Interval, Jitter, BeatTimeout,
          RegisterTimeout, ContextTimeout, MaxConsecutiveFailures, Horizon

NoBeat == -1

VARIABLES now, phase, respondBy, contextBy, lease, nextBeat, beatSentAt, failures

vars == <<now, phase, respondBy, contextBy, lease, nextBeat, beatSentAt, failures>>

Phases == {"registering", "responding", "loading_context", "running", "lost", "failed"}
Owning == {"responding", "loading_context", "running"}

NextBeatTimes == (now + Interval - Jitter)..(now + Interval + Jitter)

TypeOK ==
    /\ now \in 0..Horizon
    /\ phase \in Phases
    /\ lease \in -1..(Horizon + LeaseTTL)
    /\ failures \in 0..MaxConsecutiveFailures

Init ==
    /\ now = 0
    /\ phase = "registering"
    /\ respondBy = 0
    /\ contextBy = 0
    /\ lease = -1
    /\ nextBeat = 0
    /\ beatSentAt = NoBeat
    /\ failures = 0

RegisterCommit ==
    /\ phase = "registering"
    /\ now <= RegisterTimeout
    /\ phase' = "responding"
    /\ lease' = now + LeaseTTL
    /\ respondBy' = RegisterTimeout
    /\ UNCHANGED <<now, contextBy, nextBeat, beatSentAt, failures>>

RegisterTimedOut ==
    /\ phase = "registering"
    /\ now = RegisterTimeout
    /\ phase' = "failed"
    /\ UNCHANGED <<now, respondBy, contextBy, lease, nextBeat, beatSentAt, failures>>

RegisterResponse ==
    /\ phase = "responding"
    /\ phase' = "loading_context"
    /\ contextBy' = now + ContextTimeout
    /\ UNCHANGED <<now, respondBy, lease, nextBeat, beatSentAt, failures>>

ContextLoaded ==
    /\ phase = "loading_context"
    /\ phase' = "running"
    /\ nextBeat' \in NextBeatTimes
    /\ UNCHANGED <<now, respondBy, contextBy, lease, beatSentAt, failures>>

BeatTimer ==
    /\ phase = "running"
    /\ now = nextBeat
    /\ nextBeat' \in NextBeatTimes
    /\ beatSentAt' = IF beatSentAt = NoBeat THEN now ELSE beatSentAt
    /\ UNCHANGED <<now, phase, respondBy, contextBy, lease, failures>>

BeatAccepted ==
    /\ phase = "running"
    /\ beatSentAt # NoBeat
    /\ beatSentAt' = NoBeat
    /\ IF now <= lease + Grace
       THEN /\ lease' = now + LeaseTTL
            /\ failures' = 0
            /\ UNCHANGED phase
       ELSE /\ phase' = "lost"
            /\ UNCHANGED <<lease, failures>>
    /\ UNCHANGED <<now, respondBy, contextBy, nextBeat>>

BeatFailed ==
    /\ phase = "running"
    /\ beatSentAt # NoBeat
    /\ failures < MaxConsecutiveFailures
    /\ beatSentAt' = NoBeat
    /\ failures' = failures + 1
    /\ UNCHANGED <<now, phase, respondBy, contextBy, lease, nextBeat>>

Deadline ==
    \/ phase = "registering" /\ now >= RegisterTimeout
    \/ phase = "responding" /\ now >= respondBy
    \/ phase = "loading_context" /\ now >= contextBy
    \/ phase = "running" /\ now >= nextBeat
    \/ phase = "running" /\ beatSentAt # NoBeat /\ now >= beatSentAt + BeatTimeout

Tick ==
    /\ now < Horizon
    /\ ~Deadline
    /\ now' = now + 1
    /\ UNCHANGED <<phase, respondBy, contextBy, lease, nextBeat, beatSentAt, failures>>

Next ==
    \/ RegisterCommit
    \/ RegisterTimedOut
    \/ RegisterResponse
    \/ ContextLoaded
    \/ BeatTimer
    \/ BeatAccepted
    \/ BeatFailed
    \/ Tick

Spec == Init /\ [][Next]_vars

LeaseNeverClaimable == phase \in Owning => now <= lease + Grace

NeverLost == phase # "lost"

=============================================================================
