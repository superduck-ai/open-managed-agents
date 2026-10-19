package sessions

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestEnvironmentSessionResourceContract(t *testing.T) {
	for _, environmentType := range []string{"cloud", "self_hosted"} {
		for _, resourceType := range []string{sessionresource.FileType, sessionresource.GitRepositoryType, sessionresource.MemoryStoreType} {
			t.Run(environmentType+"/"+resourceType, func(t *testing.T) {
				env := db.Environment{ExternalID: "env_owned", Config: json.RawMessage(`{"type":"` + environmentType + `"}`)}
				resources := []normalizedSessionResource{{resource: db.SessionResource{ResourceType: resourceType}}}
				err := validateEnvironmentSessionResources(env, resources)
				rejected := environmentType == "self_hosted" && resourceType != sessionresource.MemoryStoreType
				if !rejected {
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				var app *apperr.Error
				if !errors.As(err, &app) || app.Kind != apperr.InvalidArgument {
					t.Fatalf("self-hosted %s error=%v, expected invalid request", resourceType, err)
				}
			})
		}
	}
	env := db.Environment{Config: json.RawMessage(`{"type":"self_hosted"}`)}
	if err := validateEnvironmentSessionResources(env, nil); err != nil {
		t.Fatal("empty self-hosted session rejected", err)
	}
}

func TestInvalidResourceEnvironmentConfig(t *testing.T) {
	env := db.Environment{Config: json.RawMessage(`{"type":`)}
	err := validateEnvironmentSessionResources(env, nil)
	var app *apperr.Error
	if !errors.As(err, &app) || app.Kind != apperr.Internal {
		t.Fatalf("malformed persisted environment config error=%v", err)
	}
}

func TestOnlySelfHostedMemoryUsesNewWorkCredential(t *testing.T) {
	memory := []normalizedSessionResource{{resource: db.SessionResource{ResourceType: sessionresource.MemoryStoreType}}}
	for _, tc := range []struct {
		config    string
		resources []normalizedSessionResource
		want      bool
	}{
		{`{"type":"self_hosted"}`, memory, true}, {`{"type":"cloud"}`, memory, false},
		{`{"type":"self_hosted"}`, nil, false}, {`invalid`, memory, false},
	} {
		if got := selfHostedMemoryCredentialRequired(db.Environment{Config: json.RawMessage(tc.config)}, tc.resources); got != tc.want {
			t.Fatal("credential issuance scope changed", tc.config, got)
		}
	}
}
