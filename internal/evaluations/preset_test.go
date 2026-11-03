package evaluations

import (
	"net/http"
	"strings"
	"testing"
)

func TestPresetCatalogCoversPublishedIA(t *testing.T) {
	if len(presetDatasets()) != 0 || len(presetEvaluators()) != 0 {
		t.Fatalf("datasets=%d evaluators=%d", len(presetDatasets()), len(presetEvaluators()))
	}
}

func TestListPresetsAndReusePublishedVersion(t *testing.T) {
	store := &evaluationStoreStub{}
	router := evaluationRouter(store)
	listed := serveEvaluation(router, http.MethodGet, evaluationDatasetPresetPath(""), "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"data":[]`) || strings.Contains(listed.Body.String(), "evalds_oma_demo") || strings.Contains(listed.Body.String(), `"next_page":"`) {
		t.Fatalf("list presets = %d %s", listed.Code, listed.Body.String())
	}
	missingPreset := serveEvaluation(router, http.MethodGet, evaluationDatasetPresetPath("/evalds_oma_demo_03"), "", "")
	if missingPreset.Code != http.StatusNotFound || !strings.Contains(missingPreset.Body.String(), presetDatasetNotFoundMessage) {
		t.Fatalf("missing preset = %d %s", missingPreset.Code, missingPreset.Body.String())
	}
	reused := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"from preset","desc":"copy","source_version_id":"edv_oma_demo_03_v2"}`, "")
	if reused.Code != http.StatusNotFound || !strings.Contains(reused.Body.String(), presetDatasetNotFoundMessage) || store.createDatasetCalls != 0 {
		t.Fatalf("reuse = %d calls=%d %s", reused.Code, store.createDatasetCalls, reused.Body.String())
	}
	missing := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"missing","source_version_id":"edv_missing"}`, "")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), presetDatasetNotFoundMessage) {
		t.Fatalf("missing = %d %s", missing.Code, missing.Body.String())
	}
	otherWorkspace := serveEvaluation(router, http.MethodGet, "/api/console/organizations/org_test/workspaces/other/evaluation-dataset-presets", "", "")
	if otherWorkspace.Code != http.StatusNotFound {
		t.Fatalf("other workspace = %d %s", otherWorkspace.Code, otherWorkspace.Body.String())
	}

	evaluatorList := serveEvaluation(router, http.MethodGet, evaluationEvaluatorPresetPath(""), "", "")
	if evaluatorList.Code != http.StatusOK || !strings.Contains(evaluatorList.Body.String(), `"data":[]`) || strings.Contains(evaluatorList.Body.String(), "evalev_oma_") || strings.Contains(evaluatorList.Body.String(), `"next_page":"`) {
		t.Fatalf("evaluator presets = %d %s", evaluatorList.Code, evaluatorList.Body.String())
	}
	missingEvaluatorGet := serveEvaluation(router, http.MethodGet, evaluationEvaluatorPresetPath("/evalev_oma_latency"), "", "")
	if missingEvaluatorGet.Code != http.StatusNotFound || !strings.Contains(missingEvaluatorGet.Body.String(), presetEvaluatorNotFoundMessage) {
		t.Fatalf("missing evaluator preset = %d %s", missingEvaluatorGet.Code, missingEvaluatorGet.Body.String())
	}
	withAgent := serveEvaluation(router, http.MethodPost, evaluationEvaluatorPath(""), `{"name":"copied judge","source_version_id":"eev_oma_latency_v1","agent_id":"agent_score"}`, "")
	if withAgent.Code != http.StatusBadRequest || !strings.Contains(withAgent.Body.String(), agentIDForbiddenMessage) || store.createEvaluatorCalls != 0 {
		t.Fatalf("agent id = %d calls=%d %s", withAgent.Code, store.createEvaluatorCalls, withAgent.Body.String())
	}
	copied := serveEvaluation(router, http.MethodPost, evaluationEvaluatorPath(""), `{"name":"copied judge","desc":"mine","source_version_id":"eev_oma_latency_v1"}`, "")
	if copied.Code != http.StatusNotFound || !strings.Contains(copied.Body.String(), presetEvaluatorNotFoundMessage) || store.createEvaluatorCalls != 0 {
		t.Fatalf("copy evaluator = %d calls=%d %s", copied.Code, store.createEvaluatorCalls, copied.Body.String())
	}
	conflict := serveEvaluation(router, http.MethodPost, evaluationEvaluatorPath(""), `{"name":"copied","source_version_id":"eev_oma_latency_v1","agent_id":"agent_score","model":"other"}`, "")
	if conflict.Code != http.StatusBadRequest || !strings.Contains(conflict.Body.String(), presetSourceConflictMessage) || store.createEvaluatorCalls != 0 {
		t.Fatalf("conflict = %d calls=%d %s", conflict.Code, store.createEvaluatorCalls, conflict.Body.String())
	}
	missingEvaluator := serveEvaluation(router, http.MethodPost, evaluationEvaluatorPath(""), `{"name":"missing","source_version_id":"eev_missing"}`, "")
	if missingEvaluator.Code != http.StatusNotFound || !strings.Contains(missingEvaluator.Body.String(), presetEvaluatorNotFoundMessage) || store.createEvaluatorCalls != 0 {
		t.Fatalf("missing evaluator = %d calls=%d %s", missingEvaluator.Code, store.createEvaluatorCalls, missingEvaluator.Body.String())
	}
}

func evaluationDatasetPresetPath(suffix string) string {
	return "/api/console/organizations/org_test/workspaces/workspace_test/evaluation-dataset-presets" + suffix
}

func evaluationEvaluatorPresetPath(suffix string) string {
	return "/api/console/organizations/org_test/workspaces/workspace_test/evaluation-evaluator-presets" + suffix
}
