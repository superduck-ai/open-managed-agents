package codesessions

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateClaimedWorkerEpoch(t *testing.T) {
	for _, test := range []struct {
		name         string
		claim, epoch int64
		allowed      bool
	}{
		{"failure old epoch", 2, 1, false},
		{"failure forged newer epoch", 2, 3, false},
		{"success signed epoch", 2, 2, true},
		{"success legacy unbound token", 0, 2, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := &Handler{}
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/", nil)
			allowed := h.validateClaimedWorkerEpoch(response, request, "cse_test", SessionCredentialClaims{WorkerEpoch: test.claim}, test.epoch)
			if allowed != test.allowed {
				t.Fatalf("allowed=%t want=%t", allowed, test.allowed)
			}
			if !allowed && response.Code != http.StatusConflict {
				t.Fatalf("status=%d want=409", response.Code)
			}
		})
	}
}
