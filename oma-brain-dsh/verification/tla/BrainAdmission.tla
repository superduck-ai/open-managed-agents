--------------------------- MODULE BrainAdmission ---------------------------
EXTENDS Integers, FiniteSets

CONSTANTS Sessions, Loops, MaxSessions, MaxEpoch

Epochs == 1..MaxEpoch
Objects == Sessions \X Epochs
NoObject == <<"none", 0>>

LoopPCs == {"free", "validate", "await_retire", "reserve", "register", "context",
            "create", "open", "await_open_retire", "release"}
ObjectStates == {"absent", "live", "retiring", "closed"}
LeaseStates == {"none", "claimed", "leased"}

VARIABLES pc, task, map, retiring, assignment, reserved,
          obj, stopped, hbStarted, busyObj,
          srvEpoch, srvLease

loopVars == <<pc, task>>
localVars == <<map, retiring, assignment, reserved>>
objVars == <<obj, stopped, hbStarted, busyObj>>
srvVars == <<srvEpoch, srvLease>>
vars == <<loopVars, localVars, objVars, srvVars>>

TypeOK ==
    /\ pc \in [Loops -> LoopPCs]
    /\ task \in [Loops -> Sessions \X (0..MaxEpoch)]
    /\ map \in [Sessions -> Objects \cup {NoObject}]
    /\ retiring \in [Sessions -> Objects \cup {NoObject}]
    /\ assignment \in [Sessions -> 0..MaxEpoch]
    /\ reserved \in 0..Cardinality(Loops)
    /\ obj \in [Objects -> ObjectStates]
    /\ stopped \in [Objects -> BOOLEAN]
    /\ hbStarted \in [Objects -> BOOLEAN]
    /\ busyObj \in [Objects -> BOOLEAN]
    /\ srvEpoch \in [Sessions -> 1..MaxEpoch]
    /\ srvLease \in [Sessions -> LeaseStates]

Init ==
    /\ pc = [l \in Loops |-> "free"]
    /\ task = [l \in Loops |-> <<CHOOSE s \in Sessions : TRUE, 0>>]
    /\ map = [s \in Sessions |-> NoObject]
    /\ retiring = [s \in Sessions |-> NoObject]
    /\ assignment = [s \in Sessions |-> 0]
    /\ reserved = 0
    /\ obj = [o \in Objects |-> "absent"]
    /\ stopped = [o \in Objects |-> FALSE]
    /\ hbStarted = [o \in Objects |-> FALSE]
    /\ busyObj = [o \in Objects |-> FALSE]
    /\ srvEpoch = [s \in Sessions |-> 1]
    /\ srvLease = [s \in Sessions |-> "none"]

Mapped == {map[s] : s \in Sessions} \ {NoObject}
RetiringObjs == {retiring[s] : s \in Sessions} \ {NoObject}
MapSize == Cardinality({s \in Sessions : map[s] # NoObject})
RetiringSize == Cardinality({s \in Sessions : retiring[s] # NoObject})
Claiming == Cardinality({l \in Loops : pc[l] # "free"})
Available == MaxSessions - Cardinality({o \in Mapped : busyObj[o]})

CurrentHolderHeartbeats(s) ==
    \E o \in Objects :
        /\ o[1] = s
        /\ obj[o] \in {"live", "retiring"}
        /\ ~stopped[o]
        /\ hbStarted[o]
        /\ assignment[s] = srvEpoch[s]
        /\ o[2] = srvEpoch[s]

ServerLeaseLapses(s) ==
    /\ srvLease[s] \in {"claimed", "leased"}
    /\ ~CurrentHolderHeartbeats(s)
    /\ srvLease' = [srvLease EXCEPT ![s] = "none"]
    /\ UNCHANGED <<loopVars, localVars, objVars, srvEpoch>>

RetireSync(o, m, r, st) ==
    IF m[o[1]] # o \/ r[o[1]] # NoObject
    THEN <<m, r, st>>
    ELSE <<m, [r EXCEPT ![o[1]] = o], [st EXCEPT ![o] = TRUE]>>

Claim(l) ==
    /\ pc[l] = "free"
    /\ Available - Claiming > 0
    /\ \E s \in Sessions :
          /\ srvLease[s] = "none"
          /\ srvEpoch[s] < MaxEpoch
          /\ srvEpoch' = [srvEpoch EXCEPT ![s] = @ + 1]
          /\ srvLease' = [srvLease EXCEPT ![s] = "claimed"]
          /\ task' = [task EXCEPT ![l] = <<s, srvEpoch[s] + 1>>]
          /\ pc' = [pc EXCEPT ![l] = "validate"]
    /\ UNCHANGED <<localVars, objVars>>

Validate(l) ==
    LET s == task[l][1]
        existing == map[s]
    IN
    /\ pc[l] = "validate"
    /\ IF existing = NoObject
       THEN /\ pc' = [pc EXCEPT ![l] = "reserve"]
            /\ UNCHANGED <<retiring, stopped>>
       ELSE LET res == RetireSync(existing, map, retiring, stopped) IN
            /\ retiring' = res[2]
            /\ stopped' = res[3]
            /\ obj' = IF retiring'[s] = existing
                      THEN [obj EXCEPT ![existing] = "retiring"] ELSE obj
            /\ pc' = [pc EXCEPT ![l] = "await_retire"]
    /\ (existing = NoObject) => UNCHANGED obj
    /\ UNCHANGED <<task, map, assignment, reserved, hbStarted, busyObj, srvVars>>

AwaitRetire(l) ==
    LET s == task[l][1] IN
    /\ pc[l] = "await_retire"
    /\ retiring[s] = NoObject
    /\ pc' = [pc EXCEPT ![l] = "reserve"]
    /\ UNCHANGED <<task, localVars, objVars, srvVars>>

EvictionCandidates == {o \in Mapped : ~busyObj[o] /\ retiring[o[1]] # o}

Reserve(l) ==
    LET overflow == MapSize - RetiringSize + reserved + 1 - MaxSessions IN
    /\ pc[l] = "reserve"
    /\ IF overflow <= 0
       THEN /\ reserved' = reserved + 1
            /\ pc' = [pc EXCEPT ![l] = "register"]
            /\ UNCHANGED <<retiring, obj, stopped>>
       ELSE IF Cardinality(EvictionCandidates) >= overflow
            THEN \E victims \in SUBSET EvictionCandidates :
                    /\ Cardinality(victims) = overflow
                    /\ retiring' = [s \in Sessions |->
                          IF map[s] \in victims THEN map[s] ELSE retiring[s]]
                    /\ obj' = [o \in Objects |-> IF o \in victims THEN "retiring" ELSE obj[o]]
                    /\ stopped' = [o \in Objects |-> IF o \in victims THEN TRUE ELSE stopped[o]]
                    /\ reserved' = reserved + 1
                    /\ pc' = [pc EXCEPT ![l] = "register"]
            ELSE /\ retiring' = [s \in Sessions |->
                          IF map[s] \in EvictionCandidates THEN map[s] ELSE retiring[s]]
                 /\ obj' = [o \in Objects |-> IF o \in EvictionCandidates THEN "retiring" ELSE obj[o]]
                 /\ stopped' = [o \in Objects |-> IF o \in EvictionCandidates THEN TRUE ELSE stopped[o]]
                 /\ pc' = [pc EXCEPT ![l] = "release"]
                 /\ UNCHANGED reserved
    /\ UNCHANGED <<task, map, assignment, hbStarted, busyObj, srvVars>>

Register(l) ==
    LET s == task[l][1]
        e == task[l][2]
    IN
    /\ pc[l] = "register"
    /\ IF e = srvEpoch[s]
       THEN /\ srvLease' = [srvLease EXCEPT ![s] = "leased"]
            /\ pc' = [pc EXCEPT ![l] = "context"]
            /\ UNCHANGED reserved
       ELSE /\ pc' = [pc EXCEPT ![l] = "release"]
            /\ reserved' = reserved - 1
            /\ UNCHANGED srvLease
    /\ UNCHANGED <<task, map, retiring, assignment, objVars, srvEpoch>>

LoadContext(l) ==
    /\ pc[l] = "context"
    /\ \/ pc' = [pc EXCEPT ![l] = "create"] /\ UNCHANGED reserved
       \/ pc' = [pc EXCEPT ![l] = "release"] /\ reserved' = reserved - 1
    /\ UNCHANGED <<task, map, retiring, assignment, objVars, srvVars>>

Create(l) ==
    LET s == task[l][1]
        o == task[l]
    IN
    /\ pc[l] = "create"
    /\ assignment' = [assignment EXCEPT ![s] = o[2]]
    /\ map' = [map EXCEPT ![s] = o]
    /\ obj' = [obj EXCEPT ![o] = "live"]
    /\ hbStarted' = [hbStarted EXCEPT ![o] = TRUE]
    /\ reserved' = reserved - 1
    /\ pc' = [pc EXCEPT ![l] = "open"]
    /\ UNCHANGED <<task, retiring, stopped, busyObj, srvVars>>

Open(l) ==
    LET s == task[l][1]
        o == task[l]
    IN
    /\ pc[l] = "open"
    /\ IF assignment[s] = o[2] /\ o[2] = srvEpoch[s] /\ ~stopped[o]
       THEN /\ pc' = [pc EXCEPT ![l] = "free"]
            /\ UNCHANGED <<retiring, obj, stopped>>
       ELSE LET res == RetireSync(o, map, retiring, stopped) IN
            /\ retiring' = res[2]
            /\ stopped' = [res[3] EXCEPT ![o] = TRUE]
            /\ obj' = IF retiring'[s] = o THEN [obj EXCEPT ![o] = "retiring"] ELSE obj
            /\ pc' = [pc EXCEPT ![l] = "await_open_retire"]
    /\ UNCHANGED <<task, map, assignment, reserved, hbStarted, busyObj, srvVars>>

AwaitOpenRetire(l) ==
    /\ pc[l] = "await_open_retire"
    /\ obj[task[l]] # "retiring"
    /\ pc' = [pc EXCEPT ![l] = "release"]
    /\ UNCHANGED <<task, localVars, objVars, srvVars>>

ReleaseClaim(l) ==
    LET s == task[l][1]
        e == task[l][2]
    IN
    /\ pc[l] = "release"
    /\ srvLease' = IF e = srvEpoch[s] THEN [srvLease EXCEPT ![s] = "none"] ELSE srvLease
    /\ pc' = [pc EXCEPT ![l] = "free"]
    /\ UNCHANGED <<task, localVars, objVars, srvEpoch>>

LeaseLost(o) ==
    /\ obj[o] = "live"
    /\ ~stopped[o]
    /\ hbStarted[o]
    /\ \/ assignment[o[1]] # srvEpoch[o[1]]
       \/ srvLease[o[1]] = "none"
    /\ stopped' = [stopped EXCEPT ![o] = TRUE]
    /\ UNCHANGED <<loopVars, localVars, obj, hbStarted, busyObj, srvVars>>

RetireStopped(o) ==
    /\ obj[o] = "live"
    /\ stopped[o]
    /\ map[o[1]] = o
    /\ retiring[o[1]] = NoObject
    /\ retiring' = [retiring EXCEPT ![o[1]] = o]
    /\ obj' = [obj EXCEPT ![o] = "retiring"]
    /\ UNCHANGED <<loopVars, map, assignment, reserved, stopped, hbStarted, busyObj, srvVars>>

RetireIdle(o) ==
    /\ obj[o] = "live"
    /\ ~stopped[o]
    /\ ~busyObj[o]
    /\ map[o[1]] = o
    /\ retiring[o[1]] = NoObject
    /\ retiring' = [retiring EXCEPT ![o[1]] = o]
    /\ obj' = [obj EXCEPT ![o] = "retiring"]
    /\ stopped' = [stopped EXCEPT ![o] = TRUE]
    /\ UNCHANGED <<loopVars, map, assignment, reserved, hbStarted, busyObj, srvVars>>

RetireFinish(o) ==
    LET s == o[1] IN
    /\ obj[o] = "retiring"
    /\ obj' = [obj EXCEPT ![o] = "closed"]
    /\ map' = [map EXCEPT ![s] = NoObject]
    /\ assignment' = [assignment EXCEPT ![s] = 0]
    /\ retiring' = [retiring EXCEPT ![s] = NoObject]
    /\ UNCHANGED <<loopVars, reserved, stopped, hbStarted, busyObj, srvVars>>

ToggleBusy(o) ==
    /\ obj[o] = "live"
    /\ ~stopped[o]
    /\ busyObj' = [busyObj EXCEPT ![o] = ~@]
    /\ UNCHANGED <<loopVars, localVars, obj, stopped, hbStarted, srvVars>>

Next ==
    \/ \E l \in Loops :
          \/ Claim(l) \/ Validate(l) \/ AwaitRetire(l) \/ Reserve(l)
          \/ Register(l) \/ LoadContext(l) \/ Create(l) \/ Open(l)
          \/ AwaitOpenRetire(l) \/ ReleaseClaim(l)
    \/ \E s \in Sessions : ServerLeaseLapses(s)
    \/ \E o \in Objects :
          \/ LeaseLost(o) \/ RetireStopped(o) \/ RetireIdle(o)
          \/ RetireFinish(o) \/ ToggleBusy(o)

Spec == Init /\ [][Next]_vars

LiveObjects == {o \in Objects : obj[o] = "live"}

NoOrphanSession == \A o \in LiveObjects : map[o[1]] = o

AssignmentMatchesMappedSession ==
    \A s \in Sessions :
        (map[s] # NoObject /\ obj[map[s]] = "live") => assignment[s] = map[s][2]

CapacityBound == Cardinality(LiveObjects) + reserved <= MaxSessions

ReservationAccounting == reserved = Cardinality({l \in Loops : pc[l] \in {"register", "context", "create"}})

=============================================================================
