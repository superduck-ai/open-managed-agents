---- MODULE BrainAdmission_TTrace_1794654287 ----
EXTENDS BrainAdmission_TEConstants, Sequences, TLCExt, BrainAdmission, Toolbox, Naturals, TLC

_expression ==
    LET BrainAdmission_TEExpression == INSTANCE BrainAdmission_TEExpression
    IN BrainAdmission_TEExpression!expression
----

_trace ==
    LET BrainAdmission_TETrace == INSTANCE BrainAdmission_TETrace
    IN BrainAdmission_TETrace!trace
----

_inv ==
    ~(
        TLCGet("level") = Len(_TETrace)
        /\
        stopped = ((<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE))
        /\
        retiring = ((s1 :> <<"none", 0>>))
        /\
        pc = ((l1 :> "open" @@ l2 :> "open"))
        /\
        hbStarted = ((<<s1, 1>> :> FALSE @@ <<s1, 2>> :> TRUE @@ <<s1, 3>> :> TRUE @@ <<s1, 4>> :> FALSE))
        /\
        task = ((l1 :> <<s1, 2>> @@ l2 :> <<s1, 3>>))
        /\
        srvLease = ((s1 :> "leased"))
        /\
        reserved = (0)
        /\
        assignment = ((s1 :> 3))
        /\
        obj = ((<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "live" @@ <<s1, 3>> :> "live" @@ <<s1, 4>> :> "absent"))
        /\
        srvEpoch = ((s1 :> 3))
        /\
        busyObj = ((<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE))
        /\
        map = ((s1 :> <<s1, 3>>))
    )
----

_init ==
    /\ srvLease = _TETrace[1].srvLease
    /\ assignment = _TETrace[1].assignment
    /\ stopped = _TETrace[1].stopped
    /\ pc = _TETrace[1].pc
    /\ srvEpoch = _TETrace[1].srvEpoch
    /\ retiring = _TETrace[1].retiring
    /\ reserved = _TETrace[1].reserved
    /\ map = _TETrace[1].map
    /\ obj = _TETrace[1].obj
    /\ hbStarted = _TETrace[1].hbStarted
    /\ busyObj = _TETrace[1].busyObj
    /\ task = _TETrace[1].task
----

_next ==
    /\ \E i,j \in DOMAIN _TETrace:
        /\ \/ /\ j = i + 1
              /\ i = TLCGet("level")
        /\ srvLease  = _TETrace[i].srvLease
        /\ srvLease' = _TETrace[j].srvLease
        /\ assignment  = _TETrace[i].assignment
        /\ assignment' = _TETrace[j].assignment
        /\ stopped  = _TETrace[i].stopped
        /\ stopped' = _TETrace[j].stopped
        /\ pc  = _TETrace[i].pc
        /\ pc' = _TETrace[j].pc
        /\ srvEpoch  = _TETrace[i].srvEpoch
        /\ srvEpoch' = _TETrace[j].srvEpoch
        /\ retiring  = _TETrace[i].retiring
        /\ retiring' = _TETrace[j].retiring
        /\ reserved  = _TETrace[i].reserved
        /\ reserved' = _TETrace[j].reserved
        /\ map  = _TETrace[i].map
        /\ map' = _TETrace[j].map
        /\ obj  = _TETrace[i].obj
        /\ obj' = _TETrace[j].obj
        /\ hbStarted  = _TETrace[i].hbStarted
        /\ hbStarted' = _TETrace[j].hbStarted
        /\ busyObj  = _TETrace[i].busyObj
        /\ busyObj' = _TETrace[j].busyObj
        /\ task  = _TETrace[i].task
        /\ task' = _TETrace[j].task

\* Uncomment the ASSUME below to write the states of the error trace
\* to the given file in Json format. Note that you can pass any tuple
\* to `JsonSerialize`. For example, a sub-sequence of _TETrace.
    \* ASSUME
    \*     LET J == INSTANCE Json
    \*         IN J!JsonSerialize("BrainAdmission_TTrace_1794654287.json", _TETrace)

=============================================================================

 Note that you can extract this module `BrainAdmission_TEExpression`
  to a dedicated file to reuse `expression` (the module in the 
  dedicated `BrainAdmission_TEExpression.tla` file takes precedence 
  over the module `BrainAdmission_TEExpression` below).

---- MODULE BrainAdmission_TEExpression ----
EXTENDS BrainAdmission_TEConstants, Sequences, TLCExt, BrainAdmission, Toolbox, Naturals, TLC

expression == 
    [
        \* To hide variables of the `BrainAdmission` spec from the error trace,
        \* remove the variables below.  The trace will be written in the order
        \* of the fields of this record.
        srvLease |-> srvLease
        ,assignment |-> assignment
        ,stopped |-> stopped
        ,pc |-> pc
        ,srvEpoch |-> srvEpoch
        ,retiring |-> retiring
        ,reserved |-> reserved
        ,map |-> map
        ,obj |-> obj
        ,hbStarted |-> hbStarted
        ,busyObj |-> busyObj
        ,task |-> task
        
        \* Put additional constant-, state-, and action-level expressions here:
        \* ,_stateNumber |-> _TEPosition
        \* ,_srvLeaseUnchanged |-> srvLease = srvLease'
        
        \* Format the `srvLease` variable as Json value.
        \* ,_srvLeaseJson |->
        \*     LET J == INSTANCE Json
        \*     IN J!ToJson(srvLease)
        
        \* Lastly, you may build expressions over arbitrary sets of states by
        \* leveraging the _TETrace operator.  For example, this is how to
        \* count the number of times a spec variable changed up to the current
        \* state in the trace.
        \* ,_srvLeaseModCount |->
        \*     LET F[s \in DOMAIN _TETrace] ==
        \*         IF s = 1 THEN 0
        \*         ELSE IF _TETrace[s].srvLease # _TETrace[s-1].srvLease
        \*             THEN 1 + F[s-1] ELSE F[s-1]
        \*     IN F[_TEPosition - 1]
    ]

=============================================================================



Parsing and semantic processing can take forever if the trace below is long.
 In this case, it is advised to uncomment the module below to deserialize the
 trace from a generated binary file.

\*
\*---- MODULE BrainAdmission_TETrace ----
\*EXTENDS BrainAdmission_TEConstants, IOUtils, BrainAdmission, TLC
\*
\*trace == IODeserialize("BrainAdmission_TTrace_1794654287.bin", TRUE)
\*
\*=============================================================================
\*

---- MODULE BrainAdmission_TETrace ----
EXTENDS BrainAdmission_TEConstants, BrainAdmission, TLC

trace == 
    <<
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "free" @@ l2 :> "free"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 0>> @@ l2 :> <<s1, 0>>),srvLease |-> (s1 :> "none"),reserved |-> 0,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 1),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "validate" @@ l2 :> "free"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 0>>),srvLease |-> (s1 :> "claimed"),reserved |-> 0,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 2),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "reserve" @@ l2 :> "free"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 0>>),srvLease |-> (s1 :> "claimed"),reserved |-> 0,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 2),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "register" @@ l2 :> "free"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 0>>),srvLease |-> (s1 :> "claimed"),reserved |-> 1,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 2),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "context" @@ l2 :> "free"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 0>>),srvLease |-> (s1 :> "leased"),reserved |-> 1,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 2),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "create" @@ l2 :> "free"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 0>>),srvLease |-> (s1 :> "leased"),reserved |-> 1,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 2),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "create" @@ l2 :> "free"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 0>>),srvLease |-> (s1 :> "none"),reserved |-> 1,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 2),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "create" @@ l2 :> "validate"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 3>>),srvLease |-> (s1 :> "claimed"),reserved |-> 1,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 3),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "create" @@ l2 :> "reserve"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 3>>),srvLease |-> (s1 :> "claimed"),reserved |-> 1,assignment |-> (s1 :> 0),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "absent" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 3),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<"none", 0>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "open" @@ l2 :> "reserve"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> TRUE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 3>>),srvLease |-> (s1 :> "claimed"),reserved |-> 0,assignment |-> (s1 :> 2),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "live" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 3),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<s1, 2>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "open" @@ l2 :> "register"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> TRUE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 3>>),srvLease |-> (s1 :> "claimed"),reserved |-> 1,assignment |-> (s1 :> 2),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "live" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 3),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<s1, 2>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "open" @@ l2 :> "context"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> TRUE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 3>>),srvLease |-> (s1 :> "leased"),reserved |-> 1,assignment |-> (s1 :> 2),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "live" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 3),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<s1, 2>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "open" @@ l2 :> "create"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> TRUE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 3>>),srvLease |-> (s1 :> "leased"),reserved |-> 1,assignment |-> (s1 :> 2),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "live" @@ <<s1, 3>> :> "absent" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 3),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<s1, 2>>)]),
    ([stopped |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),retiring |-> (s1 :> <<"none", 0>>),pc |-> (l1 :> "open" @@ l2 :> "open"),hbStarted |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> TRUE @@ <<s1, 3>> :> TRUE @@ <<s1, 4>> :> FALSE),task |-> (l1 :> <<s1, 2>> @@ l2 :> <<s1, 3>>),srvLease |-> (s1 :> "leased"),reserved |-> 0,assignment |-> (s1 :> 3),obj |-> (<<s1, 1>> :> "absent" @@ <<s1, 2>> :> "live" @@ <<s1, 3>> :> "live" @@ <<s1, 4>> :> "absent"),srvEpoch |-> (s1 :> 3),busyObj |-> (<<s1, 1>> :> FALSE @@ <<s1, 2>> :> FALSE @@ <<s1, 3>> :> FALSE @@ <<s1, 4>> :> FALSE),map |-> (s1 :> <<s1, 3>>)])
    >>
----


=============================================================================

---- MODULE BrainAdmission_TEConstants ----
EXTENDS BrainAdmission

CONSTANTS s1, l1, l2

=============================================================================

---- CONFIG BrainAdmission_TTrace_1794654287 ----
CONSTANTS
    Sessions = { s1 }
    Loops = { l1 , l2 }
    MaxSessions = 2
    MaxEpoch = 4
    l2 = l2
    s1 = s1
    l1 = l1

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
\* Generated on Sat Nov 14 11:04:48 UTC 2026