---- MODULE BrainClaim_TTrace_1794654319 ----
EXTENDS Sequences, BrainClaim_TEConstants, TLCExt, BrainClaim, Toolbox, Naturals, TLC

_expression ==
    LET BrainClaim_TEExpression == INSTANCE BrainClaim_TEExpression
    IN BrainClaim_TEExpression!expression
----

_trace ==
    LET BrainClaim_TETrace == INSTANCE BrainClaim_TETrace
    IN BrainClaim_TETrace!trace
----

_inv ==
    ~(
        TLCGet("level") = Len(_TETrace)
        /\
        cred = ((w1 :> 0 @@ w2 :> 0))
        /\
        lastWrite = (0)
        /\
        inputs = (2)
        /\
        active = (TRUE)
        /\
        epoch = (3)
        /\
        published = ({3})
        /\
        tool = ((w1 :> 0 @@ w2 :> 0))
        /\
        inflight = (FALSE)
        /\
        pubPhase = ("none")
        /\
        lateInput = (TRUE)
        /\
        pendingInput = (TRUE)
        /\
        dupIssue = (FALSE)
        /\
        requested = (FALSE)
        /\
        pubEpoch = (3)
        /\
        claimExp = (0)
        /\
        now = (0)
        /\
        busy = ((w1 :> FALSE @@ w2 :> FALSE))
        /\
        wst = ((w1 :> "idle" @@ w2 :> "idle"))
        /\
        lease = (-1)
        /\
        issued = ({1, 2})
        /\
        pub = ("idle")
        /\
        claimedBy = ("none")
        /\
        attempts = (0)
        /\
        staleForward = (FALSE)
    )
----

_init ==
    /\ epoch = _TETrace[1].epoch
    /\ cred = _TETrace[1].cred
    /\ active = _TETrace[1].active
    /\ attempts = _TETrace[1].attempts
    /\ pendingInput = _TETrace[1].pendingInput
    /\ wst = _TETrace[1].wst
    /\ lastWrite = _TETrace[1].lastWrite
    /\ now = _TETrace[1].now
    /\ staleForward = _TETrace[1].staleForward
    /\ issued = _TETrace[1].issued
    /\ inflight = _TETrace[1].inflight
    /\ lease = _TETrace[1].lease
    /\ tool = _TETrace[1].tool
    /\ pubPhase = _TETrace[1].pubPhase
    /\ pubEpoch = _TETrace[1].pubEpoch
    /\ dupIssue = _TETrace[1].dupIssue
    /\ pub = _TETrace[1].pub
    /\ published = _TETrace[1].published
    /\ claimedBy = _TETrace[1].claimedBy
    /\ claimExp = _TETrace[1].claimExp
    /\ lateInput = _TETrace[1].lateInput
    /\ requested = _TETrace[1].requested
    /\ inputs = _TETrace[1].inputs
    /\ busy = _TETrace[1].busy
----

_next ==
    /\ \E i,j \in DOMAIN _TETrace:
        /\ \/ /\ j = i + 1
              /\ i = TLCGet("level")
        /\ epoch  = _TETrace[i].epoch
        /\ epoch' = _TETrace[j].epoch
        /\ cred  = _TETrace[i].cred
        /\ cred' = _TETrace[j].cred
        /\ active  = _TETrace[i].active
        /\ active' = _TETrace[j].active
        /\ attempts  = _TETrace[i].attempts
        /\ attempts' = _TETrace[j].attempts
        /\ pendingInput  = _TETrace[i].pendingInput
        /\ pendingInput' = _TETrace[j].pendingInput
        /\ wst  = _TETrace[i].wst
        /\ wst' = _TETrace[j].wst
        /\ lastWrite  = _TETrace[i].lastWrite
        /\ lastWrite' = _TETrace[j].lastWrite
        /\ now  = _TETrace[i].now
        /\ now' = _TETrace[j].now
        /\ staleForward  = _TETrace[i].staleForward
        /\ staleForward' = _TETrace[j].staleForward
        /\ issued  = _TETrace[i].issued
        /\ issued' = _TETrace[j].issued
        /\ inflight  = _TETrace[i].inflight
        /\ inflight' = _TETrace[j].inflight
        /\ lease  = _TETrace[i].lease
        /\ lease' = _TETrace[j].lease
        /\ tool  = _TETrace[i].tool
        /\ tool' = _TETrace[j].tool
        /\ pubPhase  = _TETrace[i].pubPhase
        /\ pubPhase' = _TETrace[j].pubPhase
        /\ pubEpoch  = _TETrace[i].pubEpoch
        /\ pubEpoch' = _TETrace[j].pubEpoch
        /\ dupIssue  = _TETrace[i].dupIssue
        /\ dupIssue' = _TETrace[j].dupIssue
        /\ pub  = _TETrace[i].pub
        /\ pub' = _TETrace[j].pub
        /\ published  = _TETrace[i].published
        /\ published' = _TETrace[j].published
        /\ claimedBy  = _TETrace[i].claimedBy
        /\ claimedBy' = _TETrace[j].claimedBy
        /\ claimExp  = _TETrace[i].claimExp
        /\ claimExp' = _TETrace[j].claimExp
        /\ lateInput  = _TETrace[i].lateInput
        /\ lateInput' = _TETrace[j].lateInput
        /\ requested  = _TETrace[i].requested
        /\ requested' = _TETrace[j].requested
        /\ inputs  = _TETrace[i].inputs
        /\ inputs' = _TETrace[j].inputs
        /\ busy  = _TETrace[i].busy
        /\ busy' = _TETrace[j].busy

\* Uncomment the ASSUME below to write the states of the error trace
\* to the given file in Json format. Note that you can pass any tuple
\* to `JsonSerialize`. For example, a sub-sequence of _TETrace.
    \* ASSUME
    \*     LET J == INSTANCE Json
    \*         IN J!JsonSerialize("BrainClaim_TTrace_1794654319.json", _TETrace)

=============================================================================

 Note that you can extract this module `BrainClaim_TEExpression`
  to a dedicated file to reuse `expression` (the module in the 
  dedicated `BrainClaim_TEExpression.tla` file takes precedence 
  over the module `BrainClaim_TEExpression` below).

---- MODULE BrainClaim_TEExpression ----
EXTENDS Sequences, BrainClaim_TEConstants, TLCExt, BrainClaim, Toolbox, Naturals, TLC

expression == 
    [
        \* To hide variables of the `BrainClaim` spec from the error trace,
        \* remove the variables below.  The trace will be written in the order
        \* of the fields of this record.
        epoch |-> epoch
        ,cred |-> cred
        ,active |-> active
        ,attempts |-> attempts
        ,pendingInput |-> pendingInput
        ,wst |-> wst
        ,lastWrite |-> lastWrite
        ,now |-> now
        ,staleForward |-> staleForward
        ,issued |-> issued
        ,inflight |-> inflight
        ,lease |-> lease
        ,tool |-> tool
        ,pubPhase |-> pubPhase
        ,pubEpoch |-> pubEpoch
        ,dupIssue |-> dupIssue
        ,pub |-> pub
        ,published |-> published
        ,claimedBy |-> claimedBy
        ,claimExp |-> claimExp
        ,lateInput |-> lateInput
        ,requested |-> requested
        ,inputs |-> inputs
        ,busy |-> busy
        
        \* Put additional constant-, state-, and action-level expressions here:
        \* ,_stateNumber |-> _TEPosition
        \* ,_epochUnchanged |-> epoch = epoch'
        
        \* Format the `epoch` variable as Json value.
        \* ,_epochJson |->
        \*     LET J == INSTANCE Json
        \*     IN J!ToJson(epoch)
        
        \* Lastly, you may build expressions over arbitrary sets of states by
        \* leveraging the _TETrace operator.  For example, this is how to
        \* count the number of times a spec variable changed up to the current
        \* state in the trace.
        \* ,_epochModCount |->
        \*     LET F[s \in DOMAIN _TETrace] ==
        \*         IF s = 1 THEN 0
        \*         ELSE IF _TETrace[s].epoch # _TETrace[s-1].epoch
        \*             THEN 1 + F[s-1] ELSE F[s-1]
        \*     IN F[_TEPosition - 1]
    ]

=============================================================================



Parsing and semantic processing can take forever if the trace below is long.
 In this case, it is advised to uncomment the module below to deserialize the
 trace from a generated binary file.

\*
\*---- MODULE BrainClaim_TETrace ----
\*EXTENDS IOUtils, BrainClaim_TEConstants, BrainClaim, TLC
\*
\*trace == IODeserialize("BrainClaim_TTrace_1794654319.bin", TRUE)
\*
\*=============================================================================
\*

---- MODULE BrainClaim_TETrace ----
EXTENDS BrainClaim_TEConstants, BrainClaim, TLC

trace == 
    <<
    ([cred |-> (w1 :> 0 @@ w2 :> 0),lastWrite |-> 0,inputs |-> 1,active |-> TRUE,epoch |-> 1,published |-> {},tool |-> (w1 :> 0 @@ w2 :> 0),inflight |-> FALSE,pubPhase |-> "none",lateInput |-> FALSE,pendingInput |-> TRUE,dupIssue |-> FALSE,requested |-> TRUE,pubEpoch |-> 0,claimExp |-> 0,now |-> 0,busy |-> (w1 :> FALSE @@ w2 :> FALSE),wst |-> (w1 :> "idle" @@ w2 :> "idle"),lease |-> -1,issued |-> {1},pub |-> "running",claimedBy |-> "none",attempts |-> 0,staleForward |-> FALSE]),
    ([cred |-> (w1 :> 2 @@ w2 :> 0),lastWrite |-> 0,inputs |-> 1,active |-> TRUE,epoch |-> 2,published |-> {},tool |-> (w1 :> 0 @@ w2 :> 0),inflight |-> FALSE,pubPhase |-> "none",lateInput |-> FALSE,pendingInput |-> TRUE,dupIssue |-> FALSE,requested |-> TRUE,pubEpoch |-> 0,claimExp |-> 3,now |-> 0,busy |-> (w1 :> FALSE @@ w2 :> FALSE),wst |-> (w1 :> "admitting" @@ w2 :> "idle"),lease |-> -1,issued |-> {1, 2},pub |-> "running",claimedBy |-> w1,attempts |-> 1,staleForward |-> FALSE]),
    ([cred |-> (w1 :> 2 @@ w2 :> 0),lastWrite |-> 0,inputs |-> 1,active |-> TRUE,epoch |-> 2,published |-> {},tool |-> (w1 :> 0 @@ w2 :> 0),inflight |-> FALSE,pubPhase |-> "none",lateInput |-> FALSE,pendingInput |-> TRUE,dupIssue |-> FALSE,requested |-> TRUE,pubEpoch |-> 0,claimExp |-> 3,now |-> 0,busy |-> (w1 :> FALSE @@ w2 :> FALSE),wst |-> (w1 :> "rel_perm" @@ w2 :> "idle"),lease |-> -1,issued |-> {1, 2},pub |-> "running",claimedBy |-> w1,attempts |-> 1,staleForward |-> FALSE]),
    ([cred |-> (w1 :> 0 @@ w2 :> 0),lastWrite |-> 0,inputs |-> 1,active |-> TRUE,epoch |-> 3,published |-> {},tool |-> (w1 :> 0 @@ w2 :> 0),inflight |-> FALSE,pubPhase |-> "publish",lateInput |-> FALSE,pendingInput |-> TRUE,dupIssue |-> FALSE,requested |-> FALSE,pubEpoch |-> 3,claimExp |-> 2,now |-> 0,busy |-> (w1 :> FALSE @@ w2 :> FALSE),wst |-> (w1 :> "idle" @@ w2 :> "idle"),lease |-> -1,issued |-> {1, 2},pub |-> "running",claimedBy |-> "none",attempts |-> 2,staleForward |-> FALSE]),
    ([cred |-> (w1 :> 0 @@ w2 :> 0),lastWrite |-> 0,inputs |-> 2,active |-> TRUE,epoch |-> 3,published |-> {},tool |-> (w1 :> 0 @@ w2 :> 0),inflight |-> FALSE,pubPhase |-> "publish",lateInput |-> TRUE,pendingInput |-> TRUE,dupIssue |-> FALSE,requested |-> FALSE,pubEpoch |-> 3,claimExp |-> 2,now |-> 0,busy |-> (w1 :> FALSE @@ w2 :> FALSE),wst |-> (w1 :> "idle" @@ w2 :> "idle"),lease |-> -1,issued |-> {1, 2},pub |-> "running",claimedBy |-> "none",attempts |-> 2,staleForward |-> FALSE]),
    ([cred |-> (w1 :> 0 @@ w2 :> 0),lastWrite |-> 0,inputs |-> 2,active |-> TRUE,epoch |-> 3,published |-> {3},tool |-> (w1 :> 0 @@ w2 :> 0),inflight |-> FALSE,pubPhase |-> "reset",lateInput |-> TRUE,pendingInput |-> TRUE,dupIssue |-> FALSE,requested |-> FALSE,pubEpoch |-> 3,claimExp |-> 2,now |-> 0,busy |-> (w1 :> FALSE @@ w2 :> FALSE),wst |-> (w1 :> "idle" @@ w2 :> "idle"),lease |-> -1,issued |-> {1, 2},pub |-> "idle",claimedBy |-> "none",attempts |-> 2,staleForward |-> FALSE]),
    ([cred |-> (w1 :> 0 @@ w2 :> 0),lastWrite |-> 0,inputs |-> 2,active |-> TRUE,epoch |-> 3,published |-> {3},tool |-> (w1 :> 0 @@ w2 :> 0),inflight |-> FALSE,pubPhase |-> "none",lateInput |-> TRUE,pendingInput |-> TRUE,dupIssue |-> FALSE,requested |-> FALSE,pubEpoch |-> 3,claimExp |-> 0,now |-> 0,busy |-> (w1 :> FALSE @@ w2 :> FALSE),wst |-> (w1 :> "idle" @@ w2 :> "idle"),lease |-> -1,issued |-> {1, 2},pub |-> "idle",claimedBy |-> "none",attempts |-> 0,staleForward |-> FALSE])
    >>
----


=============================================================================

---- MODULE BrainClaim_TEConstants ----
EXTENDS BrainClaim

CONSTANTS w1, w2

=============================================================================

---- CONFIG BrainClaim_TTrace_1794654319 ----
CONSTANTS
    Workers = { w1 , w2 }
    MaxEpoch = 5
    MaxTime = 8
    MaxInputs = 2
    LeaseTTL = 2
    Grace = 1
    ClaimTTL = 3
    Hold = 2
    MaxAttempts = 2
    MaxBackoff = 4
    w2 = w2
    w1 = w1

INVARIANT
    _inv

CHECK_DEADLOCK
    \* CHECK_DEADLOCK off because of PROPERTY or INVARIANT above.
    FALSE

INIT
    _init

NEXT
    _next

CONSTANT
    _TETrace <- _trace

ALIAS
    _expression
=============================================================================
\* Generated on Sat Nov 14 11:05:20 UTC 2026