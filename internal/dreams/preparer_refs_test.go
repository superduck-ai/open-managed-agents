package dreams

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// P2 keeps the tested Preparer without starting workers. Method values keep
// the assemble surface reachable for dead-code until PendingWorker lands.
var preparerAssembleAPI = []any{
	NewPreparer,
	(*Preparer).ensureDreamDefaultEnvironment,
	(*Preparer).ensureDreamDefaultAgent,
	dreamSessionAgentSnapshot,
	firstConfiguredModel,
	(*Preparer).cloneMemoryStore,
	(*Preparer).cloneMemory,
	(*Preparer).createInternalSession,
	(*Preparer).transcriptRelations,
	(*db.DB).RecordPendingDreamResources,
	(*db.DB).CreateDreamSessionTranscripts,
	(*db.DB).ListDreamSessionTranscripts,
	(*db.DB).GetDreamByInternalSessionUUID,
	(*db.DB).GetDreamDefaultAgent,
	(*db.DB).RestoreDreamDefaultAgent,
	(*db.DB).GetDreamDefaultEnvironment,
	(*db.DB).RestoreDreamDefaultEnvironment,
}

func TestPreparerAssembleAPI(t *testing.T) {
	if NewPreparer(nil, nil, nil) == nil || len(preparerAssembleAPI) == 0 {
		t.Fatal("Dream preparer assemble surface is missing")
	}
}
