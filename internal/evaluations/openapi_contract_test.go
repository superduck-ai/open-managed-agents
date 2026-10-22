package evaluations

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const evaluationOpenAPIPrefix = "/api/console/organizations/{orgUuid}/workspaces/{workspaceId}"

func TestEvaluationOpenAPIContract(t *testing.T) {
	en := loadOpenAPI(t, "en")
	zh := loadOpenAPI(t, "zh")
	for _, spec := range []openAPISpec{en, zh} {
		for path := range spec.Paths {
			if strings.Contains(path, "workbench") || strings.Contains(path, "quota") {
				t.Fatalf("%s documents an out-of-scope path %s", spec.Language, path)
			}
		}
		if _, ok := spec.Components.SecuritySchemes["omaConsoleSession"]; !ok {
			t.Fatalf("%s missing console session security", spec.Language)
		}
	}
	operations := []struct {
		method string
		path   string
		id     string
	}{
		{method: "post", path: evaluationOpenAPIPrefix + "/evaluation-datasets", id: "ConsoleCreateEvaluationDataset"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-datasets", id: "ConsoleListEvaluationDatasets"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-datasets/{datasetId}", id: "ConsoleGetEvaluationDataset"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-datasets/{datasetId}/cases", id: "ConsoleListEvaluationCases"},
		{method: "post", path: evaluationOpenAPIPrefix + "/evaluation-datasets/{datasetId}/cases", id: "ConsoleCreateEvaluationCase"},
		{method: "post", path: evaluationOpenAPIPrefix + "/evaluation-datasets/{datasetId}/cases:import", id: "ConsoleImportEvaluationCases"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-dataset-presets", id: "ConsoleListEvaluationDatasetPresets"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-dataset-presets/{presetId}", id: "ConsoleGetEvaluationDatasetPreset"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-dataset-presets/{presetId}/cases", id: "ConsoleListEvaluationDatasetPresetCases"},
		{method: "post", path: evaluationOpenAPIPrefix + "/evaluation-evaluators", id: "ConsoleCreateEvaluationEvaluator"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-evaluators", id: "ConsoleListEvaluationEvaluators"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-evaluators/{evaluatorId}", id: "ConsoleGetEvaluationEvaluator"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-evaluator-presets", id: "ConsoleListEvaluationEvaluatorPresets"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-evaluator-presets/{presetId}", id: "ConsoleGetEvaluationEvaluatorPreset"},
		{method: "post", path: evaluationOpenAPIPrefix + "/evaluation-experiments", id: "ConsoleCreateEvaluationExperiment"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-experiments", id: "ConsoleListEvaluationExperiments"},
		{method: "post", path: evaluationOpenAPIPrefix + "/evaluation-experiments:compare", id: "ConsoleCompareEvaluationExperiments"},
		{method: "get", path: evaluationOpenAPIPrefix + "/evaluation-experiments/{experimentId}", id: "ConsoleGetEvaluationExperiment"},
		{method: "post", path: evaluationOpenAPIPrefix + "/evaluation-experiments/{experimentId}:start", id: "ConsoleStartEvaluationExperiment"},
	}
	docs := loadDocsNavigation(t)
	for _, spec := range []openAPISpec{en, zh} {
		for _, operation := range operations {
			assertEvaluationOperation(t, spec, docs, operation.method, operation.path, operation.id)
		}
		assertEvaluationSchemaFields(t, spec)
	}
	if en.Paths[evaluationOpenAPIPrefix+"/evaluation-datasets"]["post"].Summary == zh.Paths[evaluationOpenAPIPrefix+"/evaluation-datasets"]["post"].Summary {
		t.Fatal("Chinese OpenAPI summary was not translated")
	}
}

func assertEvaluationOperation(t *testing.T, spec openAPISpec, docs string, method, path, id string) {
	t.Helper()
	operation, ok := spec.Paths[path][method]
	if !ok || operation.OperationID != id {
		t.Fatalf("%s %s %s = %#v", spec.Language, method, path, operation)
	}
	if len(operation.Security) != 1 || len(operation.Security[0]["omaConsoleSession"]) != 0 || len(operation.Security[0]) != 1 {
		t.Fatalf("%s %s security = %#v", spec.Language, id, operation.Security)
	}
	for _, parameter := range operation.Parameters {
		if parameter.Name == "anthropic-beta" || parameter.Name == "beta" {
			t.Fatalf("%s %s uses a /v1 beta parameter", spec.Language, id)
		}
	}
	if !strings.Contains(docs, strings.ToUpper(method)+" "+path) {
		t.Fatalf("docs.json missing %s %s", strings.ToUpper(method), path)
	}
}

func assertEvaluationSchemaFields(t *testing.T, spec openAPISpec) {
	t.Helper()
	dataset := spec.Components.Schemas["EvaluationDatasetCreate"].Properties
	evaluator := spec.Components.Schemas["EvaluationEvaluatorCreate"].Properties
	experiment := spec.Components.Schemas["EvaluationExperiment"].Properties
	score := spec.Components.Schemas["EvaluationExperimentScore"].Properties
	if _, ok := dataset["source_version_id"]; !ok {
		t.Fatalf("%s dataset create missing source_version_id", spec.Language)
	}
	if _, ok := evaluator["source_version_id"]; !ok {
		t.Fatalf("%s evaluator create missing source_version_id", spec.Language)
	}
	for _, field := range []string{"scores", "evaluators", "items"} {
		if _, ok := experiment[field]; !ok {
			t.Fatalf("%s experiment missing %s", spec.Language, field)
		}
	}
	for _, field := range []string{"score", "explanation", "details", "error", "status"} {
		if _, ok := score[field]; !ok {
			t.Fatalf("%s score missing %s", spec.Language, field)
		}
	}
}

func loadOpenAPI(t *testing.T, language string) openAPISpec {
	t.Helper()
	body, err := os.ReadFile("../../openapi/oma." + language + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var spec openAPISpec
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatal(err)
	}
	spec.Language = language
	return spec
}

func loadDocsNavigation(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../docs.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

type openAPISpec struct {
	Language   string                                 `json:"-"`
	Paths      map[string]map[string]openAPIOperation `json:"paths"`
	Components openAPIComponents                      `json:"components"`
}

type openAPIComponents struct {
	Schemas         map[string]openAPISchema   `json:"schemas"`
	SecuritySchemes map[string]json.RawMessage `json:"securitySchemes"`
}

type openAPISchema struct {
	Properties map[string]json.RawMessage `json:"properties"`
}

type openAPIOperation struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Parameters  []openAPIParameter    `json:"parameters"`
	Security    []map[string][]string `json:"security"`
}

type openAPIParameter struct {
	Name string `json:"name"`
}
