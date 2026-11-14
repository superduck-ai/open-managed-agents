----------------------------- MODULE BrainClaim -----------------------------
EXTENDS Integers, FiniteSets

CONSTANTS Workers, MaxEpoch, MaxTime, MaxInputs,
          LeaseTTL, Grace, ClaimTTL, Hold, MaxAttempts, MaxBackoff

None == "none"
Null == -1

WorkerStates == {"idle", "admitting", "registered", "connected",
                 "rel_retry", "rel_handoff", "rel_idle", "rel_perm", "dead"}
Releasing == {"rel_retry", "rel_handoff", "rel_idle"}
Holding == {"admitting", "registered", "connected"}

VARIABLES now, epoch, claimedBy, claimExp, lease, requested, attempts, active,
          pub, pendingInput, inflight, inputs, lateInput,
          pubPhase, pubEpoch, published,
          wst, cred, busy, tool,
          issued, dupIssue, staleForward, lastWrite

rowVars == <<epoch, claimedBy, claimExp, lease, requested, attempts, active>>
inputVars == <<pub, pendingInput, inflight, inputs, lateInput>>
serviceVars == <<pubPhase, pubEpoch, published>>
workerVars == <<wst, cred, busy, tool>>
historyVars == <<issued, dupIssue, staleForward, lastWrite>>
vars == <<now, rowVars, inputVars, serviceVars, workerVars, historyVars>>

Backoff(a) == LET k == IF a <= 1 THEN 0 ELSE a - 1
                  p == CASE k = 0 -> 1 [] k = 1 -> 2 [] k = 2 -> 4 [] OTHER -> 8
              IN IF p > MaxBackoff THEN MaxBackoff ELSE p

Max(a, b) == IF a > b THEN a ELSE b

LeaseLapsed == lease = Null \/ lease + Grace < now

Claimable ==
    /\ active
    /\ claimExp <= now
    /\ LeaseLapsed
    /\ (requested \/ pub = "running")

CanHeartbeat(w) ==
    /\ wst[w] \in {"registered", "connected"}
    /\ cred[w] = epoch
    /\ lease # Null
    /\ now <= lease + Grace

TypeOK ==
    /\ now \in 0..MaxTime
    /\ epoch \in 1..MaxEpoch
    /\ claimedBy \in Workers \cup {None}
    /\ claimExp \in 0..(MaxTime + Hold + ClaimTTL + MaxBackoff)
    /\ lease \in {Null} \cup 0..(MaxTime + LeaseTTL)
    /\ requested \in BOOLEAN
    /\ attempts \in 0..MaxAttempts
    /\ active \in BOOLEAN
    /\ pub \in {"idle", "running"}
    /\ pendingInput \in BOOLEAN
    /\ inflight \in BOOLEAN
    /\ inputs \in 0..MaxInputs
    /\ lateInput \in BOOLEAN
    /\ pubPhase \in {"none", "publish", "reset"}
    /\ pubEpoch \in 0..MaxEpoch
    /\ published \subseteq 1..MaxEpoch
    /\ wst \in [Workers -> WorkerStates]
    /\ cred \in [Workers -> 0..MaxEpoch]
    /\ busy \in [Workers -> BOOLEAN]
    /\ tool \in [Workers -> 0..MaxEpoch]
    /\ issued \subseteq 1..MaxEpoch
    /\ dupIssue \in BOOLEAN
    /\ staleForward \in BOOLEAN
    /\ lastWrite \in 0..MaxEpoch

Init ==
    /\ now = 0
    /\ epoch = 1
    /\ claimedBy = None
    /\ claimExp = 0
    /\ lease = Null
    /\ requested = TRUE
    /\ attempts = 0
    /\ active = TRUE
    /\ pub = "running"
    /\ pendingInput = TRUE
    /\ inflight = FALSE
    /\ inputs = 1
    /\ lateInput = FALSE
    /\ pubPhase = "none"
    /\ pubEpoch = 0
    /\ published = {}
    /\ wst = [w \in Workers |-> "idle"]
    /\ cred = [w \in Workers |-> 0]
    /\ busy = [w \in Workers |-> FALSE]
    /\ tool = [w \in Workers |-> 0]
    /\ issued = {1}
    /\ dupIssue = FALSE
    /\ staleForward = FALSE
    /\ lastWrite = 0

Tick ==
    /\ now < MaxTime
    /\ now' = now + 1
    /\ UNCHANGED <<rowVars, inputVars, serviceVars, workerVars, historyVars>>

StopWorker(w) ==
    /\ wst' = [wst EXCEPT ![w] = "idle"]
    /\ cred' = [cred EXCEPT ![w] = 0]
    /\ busy' = [busy EXCEPT ![w] = FALSE]

HoldExhausted ==
    LET e2 == IF claimedBy = None THEN epoch ELSE epoch + 1 IN
    /\ e2 <= MaxEpoch
    /\ epoch' = e2
    /\ claimedBy' = None
    /\ requested' = FALSE
    /\ lease' = Null
    /\ claimExp' = now + Hold
    /\ pubPhase' = "publish"
    /\ pubEpoch' = e2
    /\ UNCHANGED <<attempts, active, published>>

ClaimRow(w, delivered) ==
    /\ epoch + 1 <= MaxEpoch
    /\ epoch' = epoch + 1
    /\ lease' = Null
    /\ claimedBy' = w
    /\ claimExp' = now + ClaimTTL
    /\ attempts' = attempts + 1
    /\ dupIssue' = (dupIssue \/ (epoch + 1) \in issued)
    /\ issued' = issued \cup {epoch + 1}
    /\ IF delivered
       THEN /\ wst' = [wst EXCEPT ![w] = "admitting"]
            /\ cred' = [cred EXCEPT ![w] = epoch + 1]
       ELSE UNCHANGED <<wst, cred>>
    /\ UNCHANGED <<requested, active>>

Claim(w) ==
    /\ wst[w] = "idle"
    /\ Claimable
    /\ \/ /\ attempts >= MaxAttempts
          /\ pubPhase = "none"
          /\ HoldExhausted
          /\ UNCHANGED <<workerVars, historyVars>>
       \/ /\ attempts < MaxAttempts
          /\ \E delivered \in BOOLEAN : ClaimRow(w, delivered)
          /\ UNCHANGED <<busy, tool, staleForward, lastWrite, serviceVars>>
    /\ UNCHANGED <<now, inputVars>>

Register(w) ==
    /\ wst[w] = "admitting"
    /\ IF epoch = cred[w] /\ active
       THEN /\ lease' = now + LeaseTTL
            /\ requested' = FALSE
            /\ claimExp' = 0
            /\ wst' = [wst EXCEPT ![w] = "registered"]
       ELSE /\ wst' = [wst EXCEPT ![w] = "rel_retry"]
            /\ UNCHANGED <<lease, requested, claimExp>>
    /\ UNCHANGED <<now, epoch, claimedBy, attempts, active, inputVars, serviceVars,
                   cred, busy, tool, historyVars>>

AdmissionFails(w) ==
    /\ wst[w] \in {"admitting", "registered"}
    /\ \E kind \in {"rel_retry", "rel_perm", "rel_handoff"} :
          wst' = [wst EXCEPT ![w] = kind]
    /\ UNCHANGED <<now, rowVars, inputVars, serviceVars, cred, busy, tool, historyVars>>

Connect(w) ==
    /\ wst[w] = "registered"
    /\ IF epoch = cred[w]
       THEN /\ attempts' = 0
            /\ wst' = [wst EXCEPT ![w] = "connected"]
            /\ UNCHANGED <<cred, busy>>
       ELSE /\ StopWorker(w)
            /\ UNCHANGED attempts
    /\ UNCHANGED <<now, epoch, claimedBy, claimExp, lease, requested, active,
                   inputVars, serviceVars, tool, historyVars>>

Heartbeat(w) ==
    /\ wst[w] \in {"registered", "connected"}
    /\ IF CanHeartbeat(w)
       THEN /\ lease' = now + LeaseTTL
            /\ UNCHANGED <<wst, cred, busy>>
       ELSE /\ StopWorker(w)
            /\ UNCHANGED lease
    /\ UNCHANGED <<now, epoch, claimedBy, claimExp, requested, attempts, active,
                   inputVars, serviceVars, tool, historyVars>>

ReleaseGuard(w) == epoch = cred[w] /\ claimedBy = w /\ active

Release(w) ==
    /\ wst[w] \in Releasing
    /\ IF ReleaseGuard(w)
       THEN /\ lease' = Null
            /\ CASE wst[w] = "rel_retry" ->
                      /\ requested' = TRUE
                      /\ claimExp' = now + Backoff(attempts)
                      /\ UNCHANGED attempts
                 [] wst[w] = "rel_handoff" ->
                      /\ requested' = TRUE
                      /\ claimExp' = 0
                      /\ attempts' = Max(attempts - 1, 0)
                 [] OTHER ->
                      /\ claimExp' = 0
                      /\ UNCHANGED <<requested, attempts>>
       ELSE UNCHANGED <<lease, requested, claimExp, attempts>>
    /\ StopWorker(w)
    /\ UNCHANGED <<now, epoch, claimedBy, active, inputVars, serviceVars, tool, historyVars>>

FailPermanently(w) ==
    /\ wst[w] = "rel_perm"
    /\ IF ReleaseGuard(w) /\ pubPhase = "none" /\ epoch + 1 <= MaxEpoch
       THEN /\ epoch' = epoch + 1
            /\ claimedBy' = None
            /\ requested' = FALSE
            /\ lease' = Null
            /\ claimExp' = now + Hold
            /\ attempts' = Max(attempts, MaxAttempts)
            /\ pubPhase' = "publish"
            /\ pubEpoch' = epoch + 1
       ELSE UNCHANGED <<epoch, claimedBy, requested, lease, claimExp, attempts, pubPhase, pubEpoch>>
    /\ StopWorker(w)
    /\ UNCHANGED <<now, active, inputVars, published, tool, historyVars>>

PublishExhausted ==
    /\ pubPhase = "publish"
    /\ \/ /\ published' = published \cup {pubEpoch}
          /\ pub' = IF pubEpoch \in published THEN pub ELSE "idle"
          /\ pubPhase' = "reset"
       \/ /\ pubPhase' = "none"
          /\ UNCHANGED <<published, pub>>
    /\ UNCHANGED <<now, rowVars, pendingInput, inflight, inputs, lateInput, pubEpoch,
                   workerVars, historyVars>>

ResetExhausted ==
    /\ pubPhase = "reset"
    /\ IF epoch = pubEpoch /\ ~requested
       THEN /\ attempts' = 0
            /\ claimExp' = 0
       ELSE UNCHANGED <<attempts, claimExp>>
    /\ pubPhase' = "none"
    /\ UNCHANGED <<now, epoch, claimedBy, lease, requested, active, inputVars,
                   pubEpoch, published, workerVars, historyVars>>

NewInput ==
    /\ inputs < MaxInputs
    /\ inputs' = inputs + 1
    /\ pendingInput' = TRUE
    /\ pub' = "running"
    /\ lateInput' = (lateInput \/ pubPhase # "none")
    /\ UNCHANGED <<now, rowVars, inflight, serviceVars, workerVars, historyVars>>

Deliver(w) ==
    /\ wst[w] = "connected"
    /\ cred[w] = epoch
    /\ ~busy[w]
    /\ pendingInput
    /\ pendingInput' = FALSE
    /\ inflight' = TRUE
    /\ busy' = [busy EXCEPT ![w] = TRUE]
    /\ UNCHANGED <<now, rowVars, pub, inputs, lateInput, serviceVars, wst, cred, tool, historyVars>>

CompleteTurn(w) ==
    /\ busy[w]
    /\ wst[w] = "connected"
    /\ IF cred[w] = epoch
       THEN /\ busy' = [busy EXCEPT ![w] = FALSE]
            /\ inflight' = FALSE
            /\ pub' = IF pendingInput THEN "running" ELSE "idle"
            /\ lateInput' = (lateInput /\ pendingInput)
            /\ lastWrite' = cred[w]
            /\ UNCHANGED <<wst, cred>>
       ELSE /\ StopWorker(w)
            /\ UNCHANGED <<inflight, pub, lateInput, lastWrite>>
    /\ UNCHANGED <<now, rowVars, pendingInput, inputs, serviceVars, tool,
                   issued, dupIssue, staleForward>>

Redeliver ==
    /\ inflight
    /\ ~\E w \in Workers : busy[w] /\ cred[w] = epoch
    /\ inflight' = FALSE
    /\ pendingInput' = TRUE
    /\ UNCHANGED <<now, rowVars, pub, inputs, lateInput, serviceVars, workerVars, historyVars>>

ToolValidate(w) ==
    /\ busy[w]
    /\ tool[w] = 0
    /\ cred[w] = epoch
    /\ tool' = [tool EXCEPT ![w] = cred[w]]
    /\ UNCHANGED <<now, rowVars, inputVars, serviceVars, wst, cred, busy, historyVars>>

ToolForward(w) ==
    /\ tool[w] # 0
    /\ staleForward' = (staleForward \/ tool[w] < epoch)
    /\ tool' = [tool EXCEPT ![w] = 0]
    /\ UNCHANGED <<now, rowVars, inputVars, serviceVars, wst, cred, busy,
                   issued, dupIssue, lastWrite>>

RetireIdle(w) ==
    /\ wst[w] = "connected"
    /\ ~busy[w]
    /\ wst' = [wst EXCEPT ![w] = "rel_idle"]
    /\ UNCHANGED <<now, rowVars, inputVars, serviceVars, cred, busy, tool, historyVars>>

Shutdown(w) ==
    /\ wst[w] = "connected"
    /\ busy[w]
    /\ wst' = [wst EXCEPT ![w] = "rel_handoff"]
    /\ busy' = [busy EXCEPT ![w] = FALSE]
    /\ UNCHANGED <<now, rowVars, inputVars, serviceVars, cred, tool, historyVars>>

Crash(w) ==
    /\ wst[w] # "dead"
    /\ wst' = [wst EXCEPT ![w] = "dead"]
    /\ cred' = [cred EXCEPT ![w] = 0]
    /\ busy' = [busy EXCEPT ![w] = FALSE]
    /\ UNCHANGED <<now, rowVars, inputVars, serviceVars, tool, historyVars>>

Restart(w) ==
    /\ wst[w] = "dead"
    /\ wst' = [wst EXCEPT ![w] = "idle"]
    /\ UNCHANGED <<now, rowVars, inputVars, serviceVars, cred, busy, tool, historyVars>>

Terminate ==
    /\ active
    /\ active' = FALSE
    /\ UNCHANGED <<now, epoch, claimedBy, claimExp, lease, requested, attempts,
                   inputVars, serviceVars, workerVars, historyVars>>

Next ==
    \/ Tick
    \/ PublishExhausted
    \/ ResetExhausted
    \/ NewInput
    \/ Redeliver
    \/ Terminate
    \/ \E w \in Workers :
          \/ Claim(w)
          \/ Register(w)
          \/ AdmissionFails(w)
          \/ Connect(w)
          \/ Heartbeat(w)
          \/ Release(w)
          \/ FailPermanently(w)
          \/ Deliver(w)
          \/ CompleteTurn(w)
          \/ ToolValidate(w)
          \/ ToolForward(w)
          \/ RetireIdle(w)
          \/ Shutdown(w)
          \/ Crash(w)
          \/ Restart(w)

Spec == Init /\ [][Next]_vars

EpochCredentialUnique == ~dupIssue

AtMostOneCurrentHolder ==
    \A w1, w2 \in Workers :
        (w1 # w2 /\ wst[w1] \in Holding /\ wst[w2] \in Holding) =>
            ~(cred[w1] = epoch /\ cred[w2] = epoch)

LeaseExclusion == ~(Claimable /\ \E w \in Workers : CanHeartbeat(w))

ExhaustionFencesHolders ==
    (claimedBy = None) => \A w \in Workers : cred[w] # epoch

AttemptsBounded == attempts <= MaxAttempts

NoStaleSandboxForward == ~staleForward

LateInputNotStranded ==
    (lateInput /\ pendingInput /\ active /\ pubPhase = "none") =>
        \/ pub = "running"
        \/ requested
        \/ \E w \in Workers : wst[w] \in Holding /\ cred[w] = epoch

EpochMonotonic == [][epoch' >= epoch]_vars
WritesMonotonic == [][lastWrite' >= lastWrite]_vars

=============================================================================
