package sessioncontract

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

const CloudHarnessRuntime = "cloud_harness"
const HarnessTurnHeader = "X-Oma-Harness-Turn"
const HarnessModelStreamHeader = "X-Oma-Harness-Model-Stream"
const HarnessModelFactsComment = ": oma-harness-model-facts "
const HarnessInputMaxBytes = 16 << 10
const HarnessToolResultMaxBytes = 64 << 10

const HarnessAskUserQuestion = "AskUserQuestion"

type HarnessToolResult struct {
	Text   string `json:"text"`
	Failed bool   `json:"failed"`
}

type HarnessUserInput struct {
	Text     string
	Metadata json.RawMessage
}

type HarnessChoiceOption struct {
	Label string `json:"label"`
}

type HarnessChoiceQuestion struct {
	ID                   string                `json:"id"`
	AlternateMultiSelect bool                  `json:"multi_select"`
	Question             string                `json:"question"`
	Options              []HarnessChoiceOption `json:"options"`
	MultiSelect          bool                  `json:"multiSelect"`
}

type HarnessChoiceResponse struct {
	ID     string `json:"custom_tool_use_id"`
	Failed bool   `json:"is_error"`
	Text   string `json:"-"`
}

func ParseHarnessChoiceQuestion(payload json.RawMessage) (HarnessChoiceQuestion, error) {
	var input struct {
		Questions []HarnessChoiceQuestion `json:"questions"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return HarnessChoiceQuestion{}, err
	}
	if len(input.Questions) != 1 {
		return HarnessChoiceQuestion{}, errors.New("cloud harness requires one question")
	}
	question := input.Questions[0]
	if strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Question) == "" || question.MultiSelect || question.AlternateMultiSelect || len(question.Options) < 2 || len(question.Options) > 4 {
		return HarnessChoiceQuestion{}, errors.New("cloud harness requires a single-select question with two to four options")
	}
	labels := make(map[string]struct{}, len(question.Options))
	for _, option := range question.Options {
		label := strings.TrimSpace(option.Label)
		if label == "" {
			return HarnessChoiceQuestion{}, errors.New("question options must have labels")
		}
		if _, duplicate := labels[label]; duplicate {
			return HarnessChoiceQuestion{}, errors.New("question options must be distinct")
		}
		labels[label] = struct{}{}
	}
	return question, nil
}

func ParseHarnessChoiceResponse(payload json.RawMessage) (HarnessChoiceResponse, error) {
	var response HarnessChoiceResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return HarnessChoiceResponse{}, err
	}
	if response.ID == "" {
		return HarnessChoiceResponse{}, errors.New("cloud harness requires a custom_tool_use_id")
	}
	text, err := harnessInputText(payload, response.Failed)
	if err != nil {
		return HarnessChoiceResponse{}, err
	}
	response.Text = text
	if response.Failed && response.Text == "" {
		response.Text = "The user declined to answer."
	}
	return response, nil
}

func ParseHarnessUserInput(payload json.RawMessage) (HarnessUserInput, error) {
	var input struct {
		Metadata json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return HarnessUserInput{}, err
	}
	if string(input.Metadata) == "null" {
		input.Metadata = nil
	}
	text, err := harnessInputText(payload, false)
	if err != nil {
		return HarnessUserInput{}, err
	}
	return HarnessUserInput{Text: text, Metadata: input.Metadata}, nil
}

func harnessInputText(payload json.RawMessage, allowEmpty bool) (string, error) {
	var message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload, &message); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, block := range message.Content {
		if block.Type != "text" {
			return "", errors.New("cloud harness supports text blocks only")
		}
		if text.Len() != 0 {
			text.WriteByte('\n')
		}
		text.WriteString(block.Text)
	}
	if !allowEmpty && text.Len() == 0 {
		return "", errors.New("cloud harness requires nonempty text")
	}
	return text.String(), nil
}

func (q HarnessChoiceQuestion) Accepts(response HarnessChoiceResponse) bool {
	if response.Failed {
		return true
	}
	label := response.Text
	var form struct {
		Answers []struct {
			ID       string   `json:"id"`
			Selected []string `json:"selected"`
			Custom   string   `json:"custom"`
		} `json:"answers"`
	}
	if err := json.Unmarshal([]byte(label), &form); err == nil && len(form.Answers) == 1 {
		answer := form.Answers[0]
		if answer.ID != q.ID {
			return false
		}
		if strings.TrimSpace(answer.Custom) != "" {
			return len(answer.Selected) == 0
		}
		if len(answer.Selected) != 1 {
			return false
		}
		label = answer.Selected[0]
	}
	return slices.ContainsFunc(q.Options, func(option HarnessChoiceOption) bool { return option.Label == label })
}
