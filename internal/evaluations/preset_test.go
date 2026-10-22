package evaluations

import (
	"net/http"
	"strings"
	"testing"
)

func TestPresetCatalogCoversPublishedIA(t *testing.T) {
	datasets := presetDatasets()
	evaluators := presetEvaluators()
	if len(datasets) != presetDatasetCount || len(evaluators) != presetEvaluatorCount {
		t.Fatalf("datasets=%d evaluators=%d", len(datasets), len(evaluators))
	}
	multiVersion := 0
	archivedDatasets := 0
	for _, dataset := range datasets {
		if dataset.Archived {
			archivedDatasets++
		}
		if len(dataset.Versions) > 1 {
			multiVersion++
		}
		if !strings.HasPrefix(dataset.Name, "OMA demo dataset ") || !strings.Contains(dataset.Description, "Not a QCA dataset") {
			t.Fatalf("dataset = %#v", dataset)
		}
	}
	llm := 0
	archivedEvaluators := 0
	for _, evaluator := range evaluators {
		if evaluator.Archived {
			archivedEvaluators++
		}
		if evaluator.EvaluatorType == evaluatorTypeLLM {
			llm++
			if !strings.Contains(evaluator.PromptTemplate, "{{") || evaluator.Model != "oma-demo" {
				t.Fatalf("llm preset = %#v", evaluator)
			}
		}
		if !strings.Contains(evaluator.Description, "Not a QCA evaluator") {
			t.Fatalf("evaluator = %#v", evaluator)
		}
	}
	if multiVersion != 1 || archivedDatasets != 1 || llm != 4 || archivedEvaluators != 1 {
		t.Fatalf("versions=%d archived datasets=%d llm=%d archived evaluators=%d", multiVersion, archivedDatasets, llm, archivedEvaluators)
	}
}

func TestListPresetsAndReusePublishedVersion(t *testing.T) {
	store := &evaluationStoreStub{}
	router := evaluationRouter(store)
	listed := serveEvaluation(router, http.MethodGet, evaluationDatasetPresetPath(""), "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"id":"evalds_oma_demo_01"`) || !strings.Contains(listed.Body.String(), `"status":"archived"`) || strings.Contains(listed.Body.String(), `"next_page":"`) {
		t.Fatalf("list presets = %d %s", listed.Code, listed.Body.String())
	}
	cases := serveEvaluation(router, http.MethodGet, evaluationDatasetPresetPath("/evalds_oma_demo_03/cases?version_id=edv_oma_demo_03_v2"), "", "")
	if cases.Code != http.StatusOK || strings.Count(cases.Body.String(), `"type":"evaluation_case"`) != 2 || !strings.Contains(cases.Body.String(), "Reply with the word ready.") {
		t.Fatalf("preset cases = %d %s", cases.Code, cases.Body.String())
	}
	checkpoints := serveEvaluation(router, http.MethodGet, evaluationDatasetPresetPath("/evalds_oma_demo_04/cases"), "", "")
	if checkpoints.Code != http.StatusOK || !strings.Contains(checkpoints.Body.String(), `"checkpoints":["ready"]`) {
		t.Fatalf("checkpoints = %d %s", checkpoints.Code, checkpoints.Body.String())
	}
	reused := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"from preset","desc":"copy","source_version_id":"edv_oma_demo_03_v2"}`, "")
	if reused.Code != http.StatusOK || store.createDatasetCalls != 1 || len(store.createdWrites) != 2 || store.createdWrites[0].Turns[0] == "" || !strings.Contains(reused.Body.String(), `"case_count":2`) || !strings.Contains(reused.Body.String(), `"type":"evaluation_dataset"`) {
		t.Fatalf("reuse = %d calls=%d writes=%#v body=%s", reused.Code, store.createDatasetCalls, store.createdWrites, reused.Body.String())
	}
	archived := serveEvaluation(router, http.MethodPost, evaluationDatasetPath(""), `{"name":"archived","source_version_id":"edv_oma_demo_19_v1"}`, "")
	if archived.Code != http.StatusBadRequest || !strings.Contains(archived.Body.String(), presetArchivedMessage) || store.createDatasetCalls != 1 {
		t.Fatalf("archived = %d calls=%d %s", archived.Code, store.createDatasetCalls, archived.Body.String())
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
	if evaluatorList.Code != http.StatusOK || !strings.Contains(evaluatorList.Body.String(), `"id":"evalev_oma_demo_01"`) || !strings.Contains(evaluatorList.Body.String(), `"evaluator_type":"agent"`) || !strings.Contains(evaluatorList.Body.String(), `"type":"evaluation_evaluator_preset"`) {
		t.Fatalf("evaluator presets = %d %s", evaluatorList.Code, evaluatorList.Body.String())
	}
	copied := serveEvaluation(router, http.MethodPost, evaluationEvaluatorPath(""), `{"name":"copied judge","desc":"mine","source_version_id":"eev_oma_demo_01_v1"}`, "")
	if copied.Code != http.StatusOK || store.createEvaluatorCalls != 1 || store.createdEvaluator.Version.EvaluatorType != evaluatorTypeLLM || store.createdEvaluator.Version.Model != "oma-demo" || !strings.Contains(store.createdEvaluator.Version.PromptTemplate, "{{input}}") {
		t.Fatalf("copy evaluator = %d evaluator=%#v body=%s", copied.Code, store.createdEvaluator, copied.Body.String())
	}
	conflict := serveEvaluation(router, http.MethodPost, evaluationEvaluatorPath(""), `{"name":"copied","source_version_id":"eev_oma_demo_01_v1","model":"other"}`, "")
	if conflict.Code != http.StatusBadRequest || !strings.Contains(conflict.Body.String(), presetSourceConflictMessage) || store.createEvaluatorCalls != 1 {
		t.Fatalf("conflict = %d calls=%d %s", conflict.Code, store.createEvaluatorCalls, conflict.Body.String())
	}
	archivedEvaluator := serveEvaluation(router, http.MethodPost, evaluationEvaluatorPath(""), `{"name":"old","source_version_id":"eev_oma_demo_11_v1"}`, "")
	if archivedEvaluator.Code != http.StatusBadRequest || !strings.Contains(archivedEvaluator.Body.String(), presetArchivedMessage) {
		t.Fatalf("archived evaluator = %d %s", archivedEvaluator.Code, archivedEvaluator.Body.String())
	}
}

func evaluationDatasetPresetPath(suffix string) string {
	return "/api/console/organizations/org_test/workspaces/workspace_test/evaluation-dataset-presets" + suffix
}

func evaluationEvaluatorPresetPath(suffix string) string {
	return "/api/console/organizations/org_test/workspaces/workspace_test/evaluation-evaluator-presets" + suffix
}
