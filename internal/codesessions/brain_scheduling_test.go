package codesessions

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestParseBrainClaimRequestRejectsInvalidReports(t *testing.T) {
	for _, body := range []string{
		`{`,
		`{"max_sessions":8,"busy_sessions":0,"resident_sessions":0,"admission_failures":0}`,
		`{"max_tasks":33,"max_sessions":8,"busy_sessions":0,"resident_sessions":0,"admission_failures":0}`,
		`{"max_tasks":1,"max_sessions":0,"busy_sessions":0,"resident_sessions":0,"admission_failures":0}`,
		`{"max_tasks":1,"max_sessions":8,"busy_sessions":-1,"resident_sessions":0,"admission_failures":0}`,
		`{"max_tasks":1,"max_sessions":8,"busy_sessions":3,"resident_sessions":2,"admission_failures":0}`,
		`{"max_tasks":1,"max_sessions":8,"busy_sessions":0,"resident_sessions":0,"admission_failures":-1}`,
	} {
		request := httptest.NewRequest("POST", "/?block_ms=25000", strings.NewReader(body))
		if _, err := parseBrainClaimRequest(httptest.NewRecorder(), request, "worker"); err == nil {
			t.Fatalf("invalid claim request accepted: %s", body)
		}
	}
	request := httptest.NewRequest("POST", "/?block_ms=25000", strings.NewReader(
		`{"max_tasks":4,"max_sessions":8,"busy_sessions":2,"resident_sessions":5,"admission_failures":1}`))
	got, err := parseBrainClaimRequest(httptest.NewRecorder(), request, "worker")
	if err != nil {
		t.Fatal(err)
	}
	want := BrainClaimRequest{
		WorkerID: "worker", Block: 25 * time.Second, MaxTasks: 4,
		Load:              db.BrainWorkerLoad{MaxSessions: 8, BusySessions: 2, ResidentSessions: 5},
		AdmissionFailures: 1,
	}
	if got != want {
		t.Fatalf("parseBrainClaimRequest() = %+v, want %+v", got, want)
	}
}

func TestBrainClaimDelayFavorsLessLoadedWorkers(t *testing.T) {
	idle := db.BrainWorkerLoad{MaxSessions: 8}
	resident := db.BrainWorkerLoad{MaxSessions: 8, ResidentSessions: 8}
	full := db.BrainWorkerLoad{MaxSessions: 8, BusySessions: 8, ResidentSessions: 8}
	if got := brainClaimDelay(full, []db.BrainWorkerLoad{full}); got != 0 {
		t.Fatalf("delay among equally loaded workers = %s", got)
	}
	if got := brainClaimDelay(idle, []db.BrainWorkerLoad{full, idle}); got != 0 {
		t.Fatalf("least loaded worker delay = %s", got)
	}
	if got := brainClaimDelay(full, []db.BrainWorkerLoad{idle, full}); got != brainClaimMaxDelay {
		t.Fatalf("busiest worker delay = %s", got)
	}
	if got := brainClaimDelay(resident, []db.BrainWorkerLoad{idle}); got != brainClaimMaxDelay/2 {
		t.Fatalf("idle resident worker delay = %s", got)
	}
	if got := brainWorkerOccupancy(db.BrainWorkerLoad{MaxSessions: 2, BusySessions: 4, ResidentSessions: 4}); got != 1 {
		t.Fatalf("overloaded occupancy = %v", got)
	}
}

func TestNextBrainWorkerCircuit(t *testing.T) {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	openUntil := now.Add(time.Minute)
	open := db.BrainWorkerCircuit{Failures: 3, OpenUntil: &openUntil}
	if circuit, changed := nextBrainWorkerCircuit(db.BrainWorkerCircuit{}, 2, now); changed || circuit.OpenUntil != nil {
		t.Fatalf("circuit below threshold = %+v, changed=%t", circuit, changed)
	}
	if circuit, changed := nextBrainWorkerCircuit(open, 3, now); changed || circuit != open {
		t.Fatalf("unchanged failures reopened circuit: %+v", circuit)
	}
	if circuit, changed := nextBrainWorkerCircuit(open, 0, now); !changed || circuit != (db.BrainWorkerCircuit{}) {
		t.Fatalf("successful admission kept circuit: %+v", circuit)
	}
	circuit, changed := nextBrainWorkerCircuit(db.BrainWorkerCircuit{}, 3, now)
	if !changed || circuit.Failures != 3 || !circuit.OpenUntil.Equal(now.Add(brainCircuitBaseHold)) {
		t.Fatalf("first open = %+v", circuit)
	}
	circuit, _ = nextBrainWorkerCircuit(circuit, 4, now)
	if !circuit.OpenUntil.Equal(now.Add(2 * brainCircuitBaseHold)) {
		t.Fatalf("failed half-open probe hold = %s", circuit.OpenUntil.Sub(now))
	}
	if got := brainCircuitHold(40); got != brainCircuitMaxHold {
		t.Fatalf("hold cap = %s", got)
	}
}

func TestNewBrainClaimPlanLimitsHalfOpenWorkers(t *testing.T) {
	now := time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC)
	openUntil := now.Add(time.Minute)
	request := BrainClaimRequest{MaxTasks: 4, Load: db.BrainWorkerLoad{MaxSessions: 8}}
	plan := newBrainClaimPlan(request, db.BrainWorkerCircuit{Failures: 3, OpenUntil: &openUntil}, nil)
	if plan.claimable(now) || !plan.claimable(openUntil) || plan.maxTasks != 1 {
		t.Fatalf("open circuit plan = %+v", plan)
	}
	plan = newBrainClaimPlan(request, db.BrainWorkerCircuit{}, nil)
	if !plan.claimable(now) || plan.maxTasks != 4 || plan.delay != 0 {
		t.Fatalf("closed circuit plan = %+v", plan)
	}
}
