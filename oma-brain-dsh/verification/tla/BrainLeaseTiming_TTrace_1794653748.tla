---- MODULE BrainLeaseTiming_TTrace_1794653748 ----
EXTENDS Sequences, TLCExt, Toolbox, BrainLeaseTiming, Naturals, TLC

_expression ==
    LET BrainLeaseTiming_TEExpression == INSTANCE BrainLeaseTiming_TEExpression
    IN BrainLeaseTiming_TEExpression!expression
----

_trace ==
    LET BrainLeaseTiming_TETrace == INSTANCE BrainLeaseTiming_TETrace
    IN BrainLeaseTiming_TETrace!trace
----

_inv ==
    ~(
        TLCGet("level") = Len(_TETrace)
        /\
        phase = ("running")
        /\
        failures = (3)
        /\
        contextBy = (1)
        /\
        beatSentAt = (127)
        /\
        respondBy = (1)
        /\
        now = (151)
        /\
        nextBeat = (156)
        /\
        lease = (100)
    )
----

_init ==
    /\ respondBy = _TETrace[1].respondBy
    /\ now = _TETrace[1].now
    /\ contextBy = _TETrace[1].contextBy
    /\ failures = _TETrace[1].failures
    /\ beatSentAt = _TETrace[1].beatSentAt
    /\ nextBeat = _TETrace[1].nextBeat
    /\ phase = _TETrace[1].phase
    /\ lease = _TETrace[1].lease
----

_next ==
    /\ \E i,j \in DOMAIN _TETrace:
        /\ \/ /\ j = i + 1
              /\ i = TLCGet("level")
        /\ respondBy  = _TETrace[i].respondBy
        /\ respondBy' = _TETrace[j].respondBy
        /\ now  = _TETrace[i].now
        /\ now' = _TETrace[j].now
        /\ contextBy  = _TETrace[i].contextBy
        /\ contextBy' = _TETrace[j].contextBy
        /\ failures  = _TETrace[i].failures
        /\ failures' = _TETrace[j].failures
        /\ beatSentAt  = _TETrace[i].beatSentAt
        /\ beatSentAt' = _TETrace[j].beatSentAt
        /\ nextBeat  = _TETrace[i].nextBeat
        /\ nextBeat' = _TETrace[j].nextBeat
        /\ phase  = _TETrace[i].phase
        /\ phase' = _TETrace[j].phase
        /\ lease  = _TETrace[i].lease
        /\ lease' = _TETrace[j].lease

\* Uncomment the ASSUME below to write the states of the error trace
\* to the given file in Json format. Note that you can pass any tuple
\* to `JsonSerialize`. For example, a sub-sequence of _TETrace.
    \* ASSUME
    \*     LET J == INSTANCE Json
    \*         IN J!JsonSerialize("BrainLeaseTiming_TTrace_1794653748.json", _TETrace)

=============================================================================

 Note that you can extract this module `BrainLeaseTiming_TEExpression`
  to a dedicated file to reuse `expression` (the module in the 
  dedicated `BrainLeaseTiming_TEExpression.tla` file takes precedence 
  over the module `BrainLeaseTiming_TEExpression` below).

---- MODULE BrainLeaseTiming_TEExpression ----
EXTENDS Sequences, TLCExt, Toolbox, BrainLeaseTiming, Naturals, TLC

expression == 
    [
        \* To hide variables of the `BrainLeaseTiming` spec from the error trace,
        \* remove the variables below.  The trace will be written in the order
        \* of the fields of this record.
        respondBy |-> respondBy
        ,now |-> now
        ,contextBy |-> contextBy
        ,failures |-> failures
        ,beatSentAt |-> beatSentAt
        ,nextBeat |-> nextBeat
        ,phase |-> phase
        ,lease |-> lease
        
        \* Put additional constant-, state-, and action-level expressions here:
        \* ,_stateNumber |-> _TEPosition
        \* ,_respondByUnchanged |-> respondBy = respondBy'
        
        \* Format the `respondBy` variable as Json value.
        \* ,_respondByJson |->
        \*     LET J == INSTANCE Json
        \*     IN J!ToJson(respondBy)
        
        \* Lastly, you may build expressions over arbitrary sets of states by
        \* leveraging the _TETrace operator.  For example, this is how to
        \* count the number of times a spec variable changed up to the current
        \* state in the trace.
        \* ,_respondByModCount |->
        \*     LET F[s \in DOMAIN _TETrace] ==
        \*         IF s = 1 THEN 0
        \*         ELSE IF _TETrace[s].respondBy # _TETrace[s-1].respondBy
        \*             THEN 1 + F[s-1] ELSE F[s-1]
        \*     IN F[_TEPosition - 1]
    ]

=============================================================================



Parsing and semantic processing can take forever if the trace below is long.
 In this case, it is advised to uncomment the module below to deserialize the
 trace from a generated binary file.

\*
\*---- MODULE BrainLeaseTiming_TETrace ----
\*EXTENDS IOUtils, BrainLeaseTiming, TLC
\*
\*trace == IODeserialize("BrainLeaseTiming_TTrace_1794653748.bin", TRUE)
\*
\*=============================================================================
\*

---- MODULE BrainLeaseTiming_TETrace ----
EXTENDS BrainLeaseTiming, TLC

trace == 
    <<
    ([phase |-> "registering",failures |-> 0,contextBy |-> 0,beatSentAt |-> -1,respondBy |-> 0,now |-> 0,nextBeat |-> 0,lease |-> -1]),
    ([phase |-> "responding",failures |-> 0,contextBy |-> 0,beatSentAt |-> -1,respondBy |-> 1,now |-> 0,nextBeat |-> 0,lease |-> 100]),
    ([phase |-> "loading_context",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 0,nextBeat |-> 0,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 0,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 1,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 2,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 3,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 4,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 5,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 6,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 7,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 8,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 9,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 10,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 11,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 12,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 13,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 14,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 15,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 16,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 17,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 18,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 19,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 20,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 21,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 22,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 23,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 24,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 25,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 26,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 27,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 28,nextBeat |-> 28,lease |-> 100]),
    ([phase |-> "running",failures |-> 0,contextBy |-> 1,beatSentAt |-> 28,respondBy |-> 1,now |-> 28,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 28,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 29,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 30,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 31,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 32,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 33,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 34,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 35,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 36,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 37,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 38,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 39,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 40,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 41,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 42,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 43,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 44,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 45,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 46,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 47,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 48,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 49,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 50,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 51,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 52,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 53,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 54,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 55,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 56,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 57,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 58,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 59,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 60,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 61,nextBeat |-> 61,lease |-> 100]),
    ([phase |-> "running",failures |-> 1,contextBy |-> 1,beatSentAt |-> 61,respondBy |-> 1,now |-> 61,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 61,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 62,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 63,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 64,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 65,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 66,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 67,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 68,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 69,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 70,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 71,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 72,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 73,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 74,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 75,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 76,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 77,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 78,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 79,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 80,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 81,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 82,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 83,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 84,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 85,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 86,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 87,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 88,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 89,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 90,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 91,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 92,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 93,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 94,nextBeat |-> 94,lease |-> 100]),
    ([phase |-> "running",failures |-> 2,contextBy |-> 1,beatSentAt |-> 94,respondBy |-> 1,now |-> 94,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 94,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 95,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 96,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 97,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 98,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 99,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 100,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 101,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 102,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 103,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 104,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 105,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 106,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 107,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 108,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 109,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 110,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 111,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 112,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 113,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 114,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 115,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 116,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 117,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 118,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 119,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 120,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 121,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 122,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 123,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 124,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 125,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 126,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> -1,respondBy |-> 1,now |-> 127,nextBeat |-> 127,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 127,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 128,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 129,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 130,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 131,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 132,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 133,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 134,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 135,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 136,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 137,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 138,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 139,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 140,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 141,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 142,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 143,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 144,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 145,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 146,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 147,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 148,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 149,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 150,nextBeat |-> 156,lease |-> 100]),
    ([phase |-> "running",failures |-> 3,contextBy |-> 1,beatSentAt |-> 127,respondBy |-> 1,now |-> 151,nextBeat |-> 156,lease |-> 100])
    >>
----


=============================================================================

---- CONFIG BrainLeaseTiming_TTrace_1794653748 ----
CONSTANTS
    LeaseTTL = 100
    Grace = 50
    Interval = 30
    Jitter = 3
    BeatTimeout = 25
    RegisterTimeout = 1
    ContextTimeout = 1
    MaxConsecutiveFailures = 3
    Horizon = 300

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
\* Generated on Sat Nov 14 10:56:05 UTC 2026