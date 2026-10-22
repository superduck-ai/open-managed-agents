package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	evaluationsapi "github.com/superduck-ai/open-managed-agents/internal/evaluations"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestEvaluationAgentScoreSandboxReadsScoreFile(t *testing.T) {
	files := &evaluationScoreFileStub{readErr: db.ErrNotFound}
	sandbox := &evaluationAgentScoreSandbox{
		create: &evaluationScoreCreatorStub{session: db.Session{ExternalID: "sesn_score", OrganizationUUID: "org", WorkspaceUUID: "ws"}},
		lookup: &evaluationScoreLookupStub{},
		files:  files,
	}
	_, err := sandbox.ReadScore(context.Background(), "org", "ws", "sesn_score")
	if !errors.Is(err, evaluationsapi.ErrAgentScoreFileMissing) {
		t.Fatalf("missing score error = %v", err)
	}
	files.readErr = nil
	files.score = []byte(`{"score":1}`)
	body, err := sandbox.ReadScore(context.Background(), "org", "ws", "sesn_score")
	if err != nil || string(body) != `{"score":1}` {
		t.Fatalf("score = (%s, %v)", body, err)
	}
}

func TestEvaluationAgentScoreSandboxStartsScoringSession(t *testing.T) {
	creator := &evaluationScoreCreatorStub{session: db.Session{ExternalID: "sesn_score", OrganizationUUID: "org", WorkspaceUUID: "ws"}}
	lookup := &evaluationScoreLookupStub{}
	files := &evaluationScoreFileStub{writeErr: errors.New("seed failed")}
	sandbox := &evaluationAgentScoreSandbox{create: creator, lookup: lookup, files: files}
	metadata := map[string]string{
		"evaluation_role":      "agent_score",
		"experiment_id":        "evalexp_test",
		"item_id":              "evalitem_test",
		"evaluator_version_id": "eev_test",
	}
	sessionID, err := sandbox.Start(context.Background(), evaluationsapi.AgentScoreInput{
		OrganizationUUID: "org", WorkspaceUUID: "ws", UserUUID: "user",
		AgentExternalID: "agent_score", AgentVersion: 7, EnvironmentExternalID: "env_test",
		ResultJSON: []byte(`{"output":"hi"}`), Turns: []string{"grade"}, Metadata: metadata,
	})
	if err != nil || sessionID != "sesn_score" {
		t.Fatalf("start = (%s, %v)", sessionID, err)
	}
	if creator.spec.AgentExternalID != "agent_score" || creator.spec.AgentVersion != 7 || creator.spec.EnvironmentExternalID != "env_test" || len(creator.spec.Resources) != 0 {
		t.Fatalf("spec = %#v", creator.spec)
	}
	if creator.principal.UserUUID != "user" || string(files.written) != `{"output":"hi"}` || files.pathHint != evaluationsapi.AgentScoreResultPath {
		t.Fatalf("principal=%#v written=%s path=%s", creator.principal, files.written, files.pathHint)
	}
	lookup.session = db.Session{OrganizationUUID: "org", WorkspaceUUID: "ws", Metadata: creator.metadata}
	claim, ok, err := sandbox.Claim(context.Background(), "org", "ws", "sesn_score")
	if err != nil || !ok || claim.ExperimentID != "evalexp_test" || claim.ItemID != "evalitem_test" || claim.VersionID != "eev_test" {
		t.Fatalf("claim = (%#v, %v, %v)", claim, ok, err)
	}
	lookup.session.Metadata = []byte(`{"experiment_id":"evalexp_test","case_id":"evalcase_test"}`)
	if _, ok, err = sandbox.Claim(context.Background(), "org", "ws", "sesn_case"); err != nil || ok {
		t.Fatalf("case claim = (%v, %v)", ok, err)
	}
}

type evaluationScoreCreatorStub struct {
	session   db.Session
	principal auth.Principal
	spec      sessionsapi.EvaluationCaseSessionSpec
	metadata  json.RawMessage
	err       error
}

func (s *evaluationScoreCreatorStub) CreateEvaluationCaseSession(_ context.Context, principal auth.Principal, spec sessionsapi.EvaluationCaseSessionSpec) (db.Session, error) {
	s.principal = principal
	s.spec = spec
	raw, err := json.Marshal(spec.Metadata)
	if err != nil {
		return db.Session{}, err
	}
	s.metadata = raw
	if s.err != nil {
		return db.Session{}, s.err
	}
	return s.session, nil
}

type evaluationScoreLookupStub struct {
	session db.Session
}

func (s *evaluationScoreLookupStub) GetSession(context.Context, string, string) (db.Session, bool, error) {
	if s.session.OrganizationUUID == "" {
		return db.Session{}, false, nil
	}
	return s.session, true, nil
}

type evaluationScoreFileStub struct {
	written  []byte
	pathHint string
	score    []byte
	writeErr error
	readErr  error
}

func (s *evaluationScoreFileStub) WriteResult(_ context.Context, _, _ string, body []byte) error {
	s.written = append([]byte(nil), body...)
	s.pathHint = evaluationsapi.AgentScoreResultPath
	return s.writeErr
}

func (s *evaluationScoreFileStub) ReadScore(context.Context, string, string) ([]byte, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	return append([]byte(nil), s.score...), nil
}
