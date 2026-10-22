package evaluations

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestCreateDatasetCopiesSelectedCases(t *testing.T) {
	store := &evaluationStoreStub{
		datasets: []db.EvaluationDataset{{
			ExternalID: "evalds_source",
			Version:    db.EvaluationDatasetVersion{UUID: "version-uuid", ExternalID: "edv_source", Version: 1},
		}},
		versionCases: []db.EvaluationCase{
			{ExternalID: "evalcase_a", Name: "alpha", Turns: []string{"one"}, Checkpoints: []byte(`["done"]`)},
			{ExternalID: "evalcase_b", Name: "beta", Turns: []string{"two"}},
		},
	}
	router := evaluationRouter(store)
	reused := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"combined","desc":"picked","source_dataset_id":" evalds_source ","case_ids":[" evalcase_b ","evalcase_a"]}`, "")
	if reused.Code != http.StatusOK || store.createDatasetCalls != 1 || store.listVersionCaseCalls != 1 || len(store.createdWrites) != 2 || store.createdWrites[0].Name != "beta" || store.createdWrites[1].Name != "alpha" || string(store.createdWrites[1].Checkpoints) != `["done"]` || !strings.Contains(reused.Body.String(), `"case_count":2`) {
		t.Fatalf("reuse = %d calls=%d lists=%d writes=%#v body=%s", reused.Code, store.createDatasetCalls, store.listVersionCaseCalls, store.createdWrites, reused.Body.String())
	}

	archivedAt := time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC)
	store.datasets[0].ArchivedAt = &archivedAt
	archived := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"archived","source_dataset_id":"evalds_source","case_ids":["evalcase_a"]}`, "")
	if archived.Code != http.StatusBadRequest || !strings.Contains(archived.Body.String(), datasetArchivedMessage) || store.createDatasetCalls != 1 || store.listVersionCaseCalls != 1 {
		t.Fatalf("archived = %d calls=%d lists=%d %s", archived.Code, store.createDatasetCalls, store.listVersionCaseCalls, archived.Body.String())
	}
	store.datasets[0].ArchivedAt = nil

	missingCase := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"missing case","source_dataset_id":"evalds_source","case_ids":["evalcase_missing"]}`, "")
	if missingCase.Code != http.StatusBadRequest || !strings.Contains(missingCase.Body.String(), caseIDsUnknownMessage) || store.createDatasetCalls != 1 {
		t.Fatalf("missing case = %d calls=%d %s", missingCase.Code, store.createDatasetCalls, missingCase.Body.String())
	}
	empty := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"empty","source_dataset_id":"evalds_source","case_ids":[]}`, "")
	if empty.Code != http.StatusBadRequest || !strings.Contains(empty.Body.String(), caseIDsRequiredMessage) || store.createDatasetCalls != 1 {
		t.Fatalf("empty = %d calls=%d %s", empty.Code, store.createDatasetCalls, empty.Body.String())
	}
	store.datasets = nil
	missingDataset := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"missing","source_dataset_id":"evalds_missing","case_ids":["evalcase_a"]}`, "")
	if missingDataset.Code != http.StatusNotFound || !strings.Contains(missingDataset.Body.String(), datasetNotFoundMessage) || store.createDatasetCalls != 1 {
		t.Fatalf("missing dataset = %d calls=%d %s", missingDataset.Code, store.createDatasetCalls, missingDataset.Body.String())
	}

	preset := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"preset subset","source_version_id":"edv_oma_demo_03_v2","case_ids":["evalcase_oma_demo_03_v2_02"]}`, "")
	if preset.Code != http.StatusOK || store.createDatasetCalls != 2 || len(store.createdWrites) != 1 || !strings.Contains(store.createdWrites[0].Turns[0], "case 2") {
		t.Fatalf("preset subset = %d calls=%d writes=%#v body=%s", preset.Code, store.createDatasetCalls, store.createdWrites, preset.Body.String())
	}
	unknownPresetCase := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"bad case","source_version_id":"edv_oma_demo_03_v2","case_ids":["evalcase_missing"]}`, "")
	if unknownPresetCase.Code != http.StatusBadRequest || !strings.Contains(unknownPresetCase.Body.String(), caseIDsUnknownMessage) || store.createDatasetCalls != 2 {
		t.Fatalf("unknown preset case = %d calls=%d %s", unknownPresetCase.Code, store.createDatasetCalls, unknownPresetCase.Body.String())
	}
	both := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"both","source_version_id":"edv_oma_demo_03_v2","source_dataset_id":"evalds_source","case_ids":["evalcase_a"]}`, "")
	if both.Code != http.StatusBadRequest || !strings.Contains(both.Body.String(), datasetSourceConflictMessage) || store.createDatasetCalls != 2 {
		t.Fatalf("both sources = %d calls=%d %s", both.Code, store.createDatasetCalls, both.Body.String())
	}
	orphan := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"orphan","case_ids":["evalcase_a"]}`, "")
	if orphan.Code != http.StatusBadRequest || !strings.Contains(orphan.Body.String(), caseIDsRequireSourceMessage) || store.createDatasetCalls != 2 {
		t.Fatalf("orphan = %d calls=%d %s", orphan.Code, store.createDatasetCalls, orphan.Body.String())
	}
	duplicate := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"duplicate","source_dataset_id":"evalds_source","case_ids":["evalcase_a","evalcase_a"]}`, "")
	if duplicate.Code != http.StatusBadRequest || !strings.Contains(duplicate.Body.String(), caseIDsInvalidMessage) || store.createDatasetCalls != 2 {
		t.Fatalf("duplicate = %d calls=%d %s", duplicate.Code, store.createDatasetCalls, duplicate.Body.String())
	}
}
