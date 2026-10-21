package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestBrainWorkerMapperBuilderContracts(t *testing.T) {
	at := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"record", mapperBuilderContract{
			statement: brainWorkerMapperRecordStatement,
			bound: buildBrainWorkerMapperRecord(yourbatis.DialectPostgres, recordBrainWorkerParams{
				ExternalID: "worker", MaxSessions: 8, BusySessions: 1, ResidentSessions: 3, AdmissionFailures: 0,
			}),
			wantID: "BrainWorkerMapper.Record", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{
				"params.ExternalID", "params.MaxSessions", "params.BusySessions", "params.ResidentSessions", "params.AdmissionFailures",
			},
			wantSQLFragments: []string{"ON CONFLICT (external_id) DO UPDATE SET", "last_seen_at = NOW()", "RETURNING circuit_failures, circuit_open_until, (xmax = 0) AS inserted"},
		}},
		{"update circuit", mapperBuilderContract{
			statement: brainWorkerMapperUpdateCircuitStatement,
			bound: buildBrainWorkerMapperUpdateCircuit(yourbatis.DialectPostgres, updateBrainWorkerCircuitParams{
				ExternalID: "worker", CircuitFailures: 3, CircuitOpenUntil: &at,
			}),
			wantID: "BrainWorkerMapper.UpdateCircuit", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"params.CircuitFailures", "params.CircuitOpenUntil", "params.ExternalID"},
			wantSQLFragments:  []string{"circuit_failures = $1", "circuit_open_until = $2", "WHERE external_id = $3"},
		}},
		{"list available loads", mapperBuilderContract{
			statement: brainWorkerMapperListAvailableLoadsStatement,
			bound:     buildBrainWorkerMapperListAvailableLoads(yourbatis.DialectPostgres, at),
			wantID:    "BrainWorkerMapper.ListAvailableLoads", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"seenAfter"},
			wantSQLFragments:  []string{"last_seen_at > $1", "circuit_open_until IS NULL OR circuit_open_until <= NOW()"},
		}},
		{"delete stale", mapperBuilderContract{
			statement: brainWorkerMapperDeleteStaleStatement,
			bound:     buildBrainWorkerMapperDeleteStale(yourbatis.DialectPostgres, at),
			wantID:    "BrainWorkerMapper.DeleteStale", wantKind: yourbatis.StatementDelete,
			wantArgumentNames: []string{"seenBefore"},
			wantSQLFragments:  []string{"DELETE FROM brain_workers", "last_seen_at < $1"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}
