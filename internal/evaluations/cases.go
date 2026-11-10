package evaluations

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const (
	maxNameLength        = 255
	maxDescriptionLength = 1024
	maxQueryLength       = 16000
	maxAnswerLength      = 16000
	maxAcceptanceLength  = 4000
	maxTraceLength       = 16000
	maxImportCases       = 1000
	maxGenerateCases     = 50
	maxInputBlocks       = 32
)

type caseDraft struct {
	Query             string
	ExpectedAnswer    string
	ExpectedToolTrace json.RawMessage
	SourceSessionID   string
	Polarity          string
	Acceptance        string
	Inputs            json.RawMessage
}

type rawCase struct {
	Query                  string          `json:"query"`
	ExpectedAnswer         string          `json:"expected_answer"`
	ExpectedAnswerCamel    string          `json:"expectedAnswer"`
	ExpectedToolTrace      json.RawMessage `json:"expected_tool_trace"`
	ExpectedToolTraceCamel json.RawMessage `json:"expectedToolTrace"`
	SourceSessionID        string          `json:"source_session_id"`
	Polarity               string          `json:"polarity"`
	Acceptance             string          `json:"acceptance"`
	Inputs                 json.RawMessage `json:"inputs"`
}

func normalizeName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", invalidArgument(nameRequiredMessage, nil)
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		return "", invalidArgument(nameTooLongMessage, nil)
	}
	return name, nil
}

func normalizeDescription(value string) (string, error) {
	description := strings.TrimSpace(value)
	if utf8.RuneCountInString(description) > maxDescriptionLength {
		return "", invalidArgument(descriptionTooLongMessage, nil)
	}
	return description, nil
}

func normalizeCase(raw rawCase) (caseDraft, error) {
	query, inputs, err := canonicalInputs(raw.Query, raw.Inputs)
	if err != nil {
		return caseDraft{}, err
	}
	draft := caseDraft{
		Query:           query,
		Inputs:          inputs,
		ExpectedAnswer:  strings.TrimSpace(firstText(raw.ExpectedAnswer, raw.ExpectedAnswerCamel)),
		SourceSessionID: strings.TrimSpace(raw.SourceSessionID),
		Polarity:        strings.TrimSpace(raw.Polarity),
		Acceptance:      strings.TrimSpace(raw.Acceptance),
	}
	if utf8.RuneCountInString(draft.ExpectedAnswer) > maxAnswerLength {
		return caseDraft{}, invalidArgument(answerTooLongMessage, nil)
	}
	if utf8.RuneCountInString(draft.Acceptance) > maxAcceptanceLength {
		return caseDraft{}, invalidArgument(acceptanceTooLongMessage, nil)
	}
	if draft.Polarity != "" && draft.Polarity != "positive" && draft.Polarity != "negative" {
		return caseDraft{}, invalidArgument(polarityInvalidMessage, nil)
	}
	trace := raw.ExpectedToolTrace
	if len(bytes.TrimSpace(trace)) == 0 {
		trace = raw.ExpectedToolTraceCamel
	}
	normalized, err := normalizeTrace(trace)
	if err != nil {
		return caseDraft{}, err
	}
	draft.ExpectedToolTrace = normalized
	return draft, nil
}

func normalizeTrace(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if len(trimmed) > maxTraceLength {
		return nil, invalidArgument(traceTooLongMessage, nil)
	}
	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return nil, invalidArgument(traceInvalidMessage, err)
	}
	switch value.(type) {
	case []any, map[string]any, string:
		return json.RawMessage(trimmed), nil
	default:
		return nil, invalidArgument(traceInvalidMessage, nil)
	}
}

func parseCaseDocument(content string) ([]caseDraft, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil, invalidArgument(importRequiredMessage, nil)
	}
	if strings.HasPrefix(trimmed, "[") {
		var rows []rawCase
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			return nil, invalidArgument(importInvalidMessage, err)
		}
		return normalizeCaseList(rows)
	}
	if strings.HasPrefix(trimmed, "{") {
		rows, wrapped, err := caseEnvelope(trimmed)
		if err != nil {
			return parseJSONL(trimmed)
		}
		if wrapped {
			return normalizeCaseList(rows)
		}
		return parseJSONL(trimmed)
	}
	return parseCSVCases(trimmed)
}

func parseJSONL(content string) ([]caseDraft, error) {
	var rows []rawCase
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row rawCase
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, invalidArgument(importInvalidMessage, err)
		}
		rows = append(rows, row)
	}
	return normalizeCaseList(rows)
}

func parseCSVCases(content string) ([]caseDraft, error) {
	reader := csv.NewReader(strings.NewReader(content))
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil || len(records) == 0 {
		return nil, invalidArgument(importInvalidMessage, err)
	}
	header := map[string]int{}
	for index, column := range records[0] {
		header[strings.TrimSpace(column)] = index
	}
	queryIndex, ok := header["query"]
	if !ok {
		return nil, invalidArgument(importInvalidMessage, nil)
	}
	rows := make([]rawCase, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(strings.TrimSpace(strings.Join(record, ""))) == 0 {
			continue
		}
		row := rawCase{Query: csvField(record, queryIndex)}
		row.ExpectedAnswer = firstText(csvField(record, headerIndex(header, "expected_answer")), csvField(record, headerIndex(header, "expectedAnswer")))
		trace := firstText(csvField(record, headerIndex(header, "expected_tool_trace")), csvField(record, headerIndex(header, "expectedToolTrace")))
		if strings.TrimSpace(trace) != "" {
			row.ExpectedToolTrace = json.RawMessage(trace)
		}
		row.SourceSessionID = firstText(csvField(record, headerIndex(header, "source_session_id")), csvField(record, headerIndex(header, "sourceSessionId")))
		row.Polarity = csvField(record, headerIndex(header, "polarity"))
		row.Acceptance = csvField(record, headerIndex(header, "acceptance"))
		rows = append(rows, row)
	}
	return normalizeCaseList(rows)
}

func headerIndex(header map[string]int, name string) int {
	index, ok := header[name]
	if !ok {
		return -1
	}
	return index
}

func csvField(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return record[index]
}

type caseUpdateRequest struct {
	Query                  string           `json:"query"`
	ExpectedAnswer         string           `json:"expected_answer"`
	ExpectedAnswerCamel    string           `json:"expectedAnswer"`
	ExpectedToolTrace      *json.RawMessage `json:"expected_tool_trace"`
	ExpectedToolTraceCamel *json.RawMessage `json:"expectedToolTrace"`
	SourceSessionID        string           `json:"source_session_id"`
	Polarity               string           `json:"polarity"`
	Acceptance             string           `json:"acceptance"`
	Inputs                 json.RawMessage  `json:"inputs"`
}

func (body caseUpdateRequest) keepsTrace() bool {
	return body.ExpectedToolTrace == nil && body.ExpectedToolTraceCamel == nil
}

func (body caseUpdateRequest) draft() (caseDraft, error) {
	raw := rawCase{
		Query: body.Query, ExpectedAnswer: body.ExpectedAnswer, ExpectedAnswerCamel: body.ExpectedAnswerCamel,
		SourceSessionID: body.SourceSessionID, Polarity: body.Polarity, Acceptance: body.Acceptance,
		Inputs: body.Inputs,
	}
	if body.ExpectedToolTrace != nil {
		raw.ExpectedToolTrace = *body.ExpectedToolTrace
	}
	if body.ExpectedToolTraceCamel != nil {
		raw.ExpectedToolTraceCamel = *body.ExpectedToolTraceCamel
	}
	return normalizeCase(raw)
}

func caseEnvelope(content string) ([]rawCase, bool, error) {
	var envelope struct {
		Cases json.RawMessage `json:"cases"`
	}
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return nil, false, err
	}
	if len(bytes.TrimSpace(envelope.Cases)) == 0 {
		return nil, false, nil
	}
	var rows []rawCase
	if err := json.Unmarshal(envelope.Cases, &rows); err != nil {
		return nil, false, err
	}
	return rows, true, nil
}

func optionalCases(rows []rawCase) ([]caseDraft, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	return normalizeCaseList(rows)
}

func normalizeCaseList(rows []rawCase) ([]caseDraft, error) {
	if len(rows) == 0 {
		return nil, invalidArgument(importRequiredMessage, nil)
	}
	if len(rows) > maxImportCases {
		return nil, invalidArgument(importTooLargeMessage, nil)
	}
	drafts := make([]caseDraft, 0, len(rows))
	for _, row := range rows {
		draft, err := normalizeCase(row)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, draft)
	}
	return drafts, nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
