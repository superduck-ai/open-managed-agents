package evaluations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

const (
	polarityPositive        = "positive"
	polarityNegative        = "negative"
	maxPlanScenarios        = 12
	maxScenarioName         = 80
	maxScenarioPersona      = 200
	maxScenarioDescription  = 1000
	draftMaxTokens          = 8192
	draftBatchSize          = 5
	draftConcurrency        = 4
	generationRequestLimit  = 1 << 16
	generatedScenarioPrefix = "场景 "
)

var (
	errPlanEmpty        = errors.New("plan has no scenarios")
	errDraftsUnreadable = errors.New("generated cases are unreadable")
)

type casePlanRequest struct {
	generationSource
	Count int `json:"count"`
}

type caseDraftRequest struct {
	generationSource
	Scenarios []caseScenario `json:"scenarios"`
}

type caseScenario struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Persona     string `json:"persona"`
	Polarity    string `json:"polarity"`
	Count       int    `json:"count"`
}

type casePlanResponse struct {
	Type      string         `json:"type"`
	Scenarios []caseScenario `json:"scenarios"`
}

type caseDraftResponse struct {
	Query             string          `json:"query"`
	ExpectedAnswer    string          `json:"expected_answer"`
	ExpectedToolTrace json.RawMessage `json:"expected_tool_trace"`
	Polarity          string          `json:"polarity"`
	Acceptance        string          `json:"acceptance"`
	Scenario          string          `json:"scenario"`
}

type caseDraftsResponse struct {
	Type       string              `json:"type"`
	Data       []caseDraftResponse `json:"data"`
	Requested  int                 `json:"requested"`
	Duplicates int                 `json:"duplicates"`
	Failed     int                 `json:"failed"`
}

type draftBatch struct {
	scenario caseScenario
	count    int
}

type generatedCase struct {
	draft    caseDraft
	scenario string
}

type batchResult struct {
	cases []generatedCase
	err   error
}

func (h *Handler) planCases(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[casePlanRequest](w, r, generationRequestLimit)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	if body.Count < 1 || body.Count > maxGenerateCases {
		return invalidArgument(generateCountMessage, nil)
	}
	loaded, err := h.generationInputs(r.Context(), principal, body.generationSource)
	if err != nil {
		return err
	}
	text, err := h.complete(r.Context(), CompletionRequest{
		OrganizationUUID: principal.OrganizationUUID,
		System:           jsonObjectSystemPrompt, User: casePlanPrompt(loaded, body.Count),
	})
	if err != nil {
		return err
	}
	scenarios, err := parsePlan(text, body.Count)
	if err != nil {
		return internalFailure(generateFailedMessage, err)
	}
	httpapi.WriteJSON(w, http.StatusOK, casePlanResponse{Type: "evaluation_case_plan", Scenarios: scenarios})
	return nil
}

func (h *Handler) draftCases(w http.ResponseWriter, r *http.Request) error {
	principal, err := scopedPrincipal(r)
	if err != nil {
		return err
	}
	body, err := httpapi.DecodeObjectBodyAs[caseDraftRequest](w, r, generationRequestLimit)
	if err != nil {
		return invalidArgument(invalidJSONMessage, err)
	}
	scenarios, total, err := normalizeRequestedScenarios(body.Scenarios)
	if err != nil {
		return err
	}
	loaded, err := h.generationInputs(r.Context(), principal, body.generationSource)
	if err != nil {
		return err
	}
	results := h.runDraftBatches(r.Context(), principal, loaded, draftBatches(scenarios))
	cases, err := h.collectDrafts(r.Context(), results)
	if err != nil {
		return err
	}
	kept, duplicates := dedupeGenerated(cases, loaded.existing)
	httpapi.WriteJSON(w, http.StatusOK, caseDraftsResponse{
		Type: "evaluation_case_drafts", Data: draftResponses(kept), Requested: total,
		Duplicates: duplicates, Failed: total - len(cases),
	})
	return nil
}

func (h *Handler) generationInputs(ctx context.Context, principal auth.Principal, source generationSource) (generationContext, error) {
	normalized, err := source.normalized()
	if err != nil {
		return generationContext{}, err
	}
	return h.loadGenerationContext(ctx, principal, normalized)
}

func parsePlan(text string, total int) ([]caseScenario, error) {
	raw, err := extractJSONObject(stripCodeFence(text))
	if err != nil {
		return nil, err
	}
	var document struct {
		Scenarios []caseScenario `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	scenarios := make([]caseScenario, 0, len(document.Scenarios))
	for _, scenario := range document.Scenarios {
		if cleaned, ok := modelScenario(scenario, len(scenarios)+1); ok {
			scenarios = append(scenarios, cleaned)
		}
		if len(scenarios) == maxPlanScenarios {
			break
		}
	}
	if len(scenarios) == 0 {
		return nil, errPlanEmpty
	}
	return rebalanceScenarios(scenarios, total), nil
}

func modelScenario(scenario caseScenario, position int) (caseScenario, bool) {
	name := clipRunes(strings.TrimSpace(scenario.Name), maxScenarioName)
	description := clipRunes(strings.TrimSpace(scenario.Description), maxScenarioDescription)
	if name == "" && description == "" {
		return caseScenario{}, false
	}
	if name == "" {
		name = generatedScenarioPrefix + strconv.Itoa(position)
	}
	polarity := polarityPositive
	if strings.TrimSpace(scenario.Polarity) == polarityNegative {
		polarity = polarityNegative
	}
	return caseScenario{
		Name: name, Description: description, Persona: clipRunes(strings.TrimSpace(scenario.Persona), maxScenarioPersona),
		Polarity: polarity, Count: max(scenario.Count, 1),
	}, true
}

func rebalanceScenarios(scenarios []caseScenario, total int) []caseScenario {
	if len(scenarios) > total {
		scenarios = scenarios[:total]
	}
	sum := 0
	for _, scenario := range scenarios {
		sum += scenario.Count
	}
	assigned := 0
	for index := range scenarios {
		scenarios[index].Count = max(1, scenarios[index].Count*total/sum)
		assigned += scenarios[index].Count
	}
	for assigned > total {
		scenarios[largestScenario(scenarios)].Count--
		assigned--
	}
	for index := 0; assigned < total; index++ {
		scenarios[index%len(scenarios)].Count++
		assigned++
	}
	return scenarios
}

func largestScenario(scenarios []caseScenario) int {
	largest := 0
	for index, scenario := range scenarios {
		if scenario.Count > scenarios[largest].Count {
			largest = index
		}
	}
	return largest
}

func normalizeRequestedScenarios(input []caseScenario) ([]caseScenario, int, error) {
	if len(input) == 0 {
		return nil, 0, invalidArgument(scenariosRequiredMessage, nil)
	}
	if len(input) > maxPlanScenarios {
		return nil, 0, invalidArgument(scenariosTooManyMessage, nil)
	}
	scenarios := make([]caseScenario, 0, len(input))
	total := 0
	for _, scenario := range input {
		cleaned, err := requestedScenario(scenario)
		if err != nil {
			return nil, 0, err
		}
		total += cleaned.Count
		scenarios = append(scenarios, cleaned)
	}
	if total > maxGenerateCases {
		return nil, 0, invalidArgument(scenariosTooManyMessage, nil)
	}
	return scenarios, total, nil
}

func requestedScenario(scenario caseScenario) (caseScenario, error) {
	cleaned := caseScenario{
		Name: strings.TrimSpace(scenario.Name), Description: strings.TrimSpace(scenario.Description),
		Persona: strings.TrimSpace(scenario.Persona), Polarity: strings.TrimSpace(scenario.Polarity), Count: scenario.Count,
	}
	if cleaned.Name == "" || cleaned.Count < 1 || (cleaned.Polarity != polarityPositive && cleaned.Polarity != polarityNegative) {
		return caseScenario{}, invalidArgument(scenarioInvalidMessage, nil)
	}
	if utf8.RuneCountInString(cleaned.Name) > maxScenarioName ||
		utf8.RuneCountInString(cleaned.Persona) > maxScenarioPersona ||
		utf8.RuneCountInString(cleaned.Description) > maxScenarioDescription {
		return caseScenario{}, invalidArgument(scenarioTooLongMessage, nil)
	}
	return cleaned, nil
}

func draftBatches(scenarios []caseScenario) []draftBatch {
	var batches []draftBatch
	for _, scenario := range scenarios {
		for remaining := scenario.Count; remaining > 0; remaining -= draftBatchSize {
			batches = append(batches, draftBatch{scenario: scenario, count: min(remaining, draftBatchSize)})
		}
	}
	return batches
}

func (h *Handler) runDraftBatches(ctx context.Context, principal auth.Principal, loaded generationContext, batches []draftBatch) []batchResult {
	results := make([]batchResult, len(batches))
	slots := make(chan struct{}, draftConcurrency)
	var group sync.WaitGroup
	for index, batch := range batches {
		group.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			results[index] = h.draftBatch(ctx, principal, loaded, batch)
		})
	}
	group.Wait()
	return results
}

func (h *Handler) draftBatch(ctx context.Context, principal auth.Principal, loaded generationContext, batch draftBatch) batchResult {
	text, err := h.complete(ctx, CompletionRequest{
		OrganizationUUID: principal.OrganizationUUID,
		System:           jsonArraySystemPrompt, User: caseDraftPrompt(loaded, batch), MaxTokens: draftMaxTokens,
	})
	if err != nil {
		return batchResult{err: err}
	}
	rows, err := generatedRows(text)
	if err != nil {
		return batchResult{err: internalFailure(generateFailedMessage, err)}
	}
	return batchResult{cases: generatedCases(rows, batch, loaded.agent != "")}
}

func generatedRows(text string) ([]rawCase, error) {
	trimmed := strings.TrimSpace(stripCodeFence(text))
	if strings.HasPrefix(trimmed, "{") {
		rows, wrapped, err := caseEnvelope(trimmed)
		if err != nil || !wrapped {
			return nil, errors.Join(errDraftsUnreadable, err)
		}
		return rows, nil
	}
	start := strings.Index(trimmed, "[")
	end := strings.LastIndex(trimmed, "]")
	if start < 0 || end <= start {
		return nil, errDraftsUnreadable
	}
	var rows []rawCase
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func generatedCases(rows []rawCase, batch draftBatch, keepTools bool) []generatedCase {
	cases := make([]generatedCase, 0, batch.count)
	for _, row := range rows {
		if len(cases) == batch.count {
			break
		}
		row.Polarity = batch.scenario.Polarity
		row.SourceSessionID = ""
		row.Inputs = nil
		row.Files = nil
		if batch.scenario.Polarity == polarityPositive {
			row.Acceptance = ""
		}
		if !keepTools || string(bytes.TrimSpace(row.ExpectedToolTrace)) == "[]" {
			row.ExpectedToolTrace = nil
			row.ExpectedToolTraceCamel = nil
		}
		draft, err := normalizeCase(row)
		if err != nil {
			continue
		}
		cases = append(cases, generatedCase{draft: draft, scenario: batch.scenario.Name})
	}
	return cases
}

func (h *Handler) collectDrafts(ctx context.Context, results []batchResult) ([]generatedCase, error) {
	var cases []generatedCase
	var firstErr error
	for index, result := range results {
		if result.err != nil {
			h.logger.WarnContext(ctx, "evaluation case batch failed", "batch_index", index, "error", result.err)
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		cases = append(cases, result.cases...)
	}
	if len(cases) == 0 && firstErr != nil {
		return nil, firstErr
	}
	if len(cases) == 0 {
		return nil, internalFailure(generateFailedMessage, errPlanEmpty)
	}
	return cases, nil
}

func dedupeGenerated(cases []generatedCase, existing []string) ([]generatedCase, int) {
	filter := newDuplicateFilter(existing)
	kept := make([]generatedCase, 0, len(cases))
	for _, item := range cases {
		if filter.admit(item.draft.Query) {
			kept = append(kept, item)
		}
	}
	return kept, len(cases) - len(kept)
}

func draftResponses(cases []generatedCase) []caseDraftResponse {
	data := make([]caseDraftResponse, 0, len(cases))
	for _, item := range cases {
		trace := item.draft.ExpectedToolTrace
		if len(trace) == 0 {
			trace = json.RawMessage(`null`)
		}
		data = append(data, caseDraftResponse{
			Query: item.draft.Query, ExpectedAnswer: item.draft.ExpectedAnswer, ExpectedToolTrace: trace,
			Polarity: item.draft.Polarity, Acceptance: item.draft.Acceptance, Scenario: item.scenario,
		})
	}
	return data
}
