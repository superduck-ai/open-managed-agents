//go:build integration

package db

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEvaluationCaseJSONRoundTrip(t *testing.T) {
	ctx, _, database := newCodeSessionClaimTestStore(t)
	now := time.Now().UTC()
	org := "53100000-0000-0000-0000-000000000001"
	workspace := "53100000-0000-0000-0000-000000000002"
	dataset := EvaluationDataset{
		UUID: "53100000-0000-0000-0000-000000000010", ExternalID: "evalds_json",
		OrganizationUUID: org, WorkspaceUUID: workspace, Name: "json",
		VersionUUID: "53100000-0000-0000-0000-000000000011", VersionExternalID: "edv_json", CreatedAt: now,
	}
	emptyInputs := json.RawMessage(`[]`)
	imageInputs := json.RawMessage(`[{"source":{"file_id":"file_chart","type":"file"},"type":"image"}]`)
	trace := json.RawMessage(`["lookup_sales"]`)
	_, err := database.CreateEvaluationDataset(ctx, dataset, []EvaluationCaseWrite{
		{UUID: "53100000-0000-0000-0000-000000000012", ExternalID: "evalcase_empty", Query: "没有轨迹", Inputs: emptyInputs},
		{UUID: "53100000-0000-0000-0000-000000000013", ExternalID: "evalcase_trace", Query: "有轨迹", ExpectedToolTrace: trace, Inputs: imageInputs},
	})
	if err != nil {
		t.Fatal(err)
	}

	listed, err := database.ListEvaluationCases(ctx, org, workspace, dataset.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]EvaluationCase{}
	for _, item := range listed {
		byID[item.ExternalID] = item
	}
	assertJSON(t, byID["evalcase_empty"].ExpectedToolTrace, nil)
	assertJSON(t, byID["evalcase_empty"].Inputs, emptyInputs)
	assertJSON(t, byID["evalcase_trace"].ExpectedToolTrace, trace)
	assertJSON(t, byID["evalcase_trace"].Inputs, imageInputs)

	updated, err := database.UpdateEvaluationCase(ctx, org, workspace, dataset.ExternalID, EvaluationCaseWrite{
		ExternalID: "evalcase_empty", Query: "补上轨迹", ExpectedToolTrace: trace, Inputs: imageInputs,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, updated.ExpectedToolTrace, trace)
	assertJSON(t, updated.Inputs, imageInputs)

	cleared, err := database.UpdateEvaluationCase(ctx, org, workspace, dataset.ExternalID, EvaluationCaseWrite{
		ExternalID: "evalcase_trace", Query: "清掉轨迹", Inputs: emptyInputs,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	assertJSON(t, cleared.ExpectedToolTrace, nil)
	assertJSON(t, cleared.Inputs, emptyInputs)
}

func assertJSON(t *testing.T, got, want []byte) {
	t.Helper()
	if len(want) == 0 {
		if len(got) != 0 {
			t.Fatalf("json = %s, want empty", got)
		}
		return
	}
	var left, right any
	if err := json.Unmarshal(got, &left); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &right); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("json = %s, want %s", got, want)
	}
}
