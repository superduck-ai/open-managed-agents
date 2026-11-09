package evaluations

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

var (
	errScoreParse   = errors.New("score parse")
	errModelMissing = errors.New("model missing")
	errModelFailed  = errors.New("model failed")
)

type parsedScore struct {
	Score       float64
	Passed      bool
	Explanation string
	Dimensions  json.RawMessage
}

type scoreDocument struct {
	Score       float64         `json:"score"`
	Passed      *bool           `json:"passed"`
	Explanation string          `json:"explanation"`
	Dimensions  json.RawMessage `json:"dimensions"`
}

func parseScoreText(text string, passScore int) (parsedScore, error) {
	raw, err := extractJSONObject(text)
	if err != nil {
		return parsedScore{}, err
	}
	var document scoreDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return parsedScore{}, err
	}
	score := document.Score
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	passed := score >= float64(passScore)
	if document.Passed != nil {
		passed = *document.Passed
	}
	dimensions := json.RawMessage(`[]`)
	if len(bytes.TrimSpace(document.Dimensions)) > 0 && json.Valid(document.Dimensions) {
		dimensions = append(json.RawMessage(nil), document.Dimensions...)
	}
	explanation := document.Explanation
	if len(explanation) > 4000 {
		explanation = explanation[:4000]
	}
	return parsedScore{Score: score, Passed: passed, Explanation: explanation, Dimensions: dimensions}, nil
}

func extractJSONObject(text string) (json.RawMessage, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, errScoreParse
	}
	raw := json.RawMessage(text[start : end+1])
	if !json.Valid(raw) {
		return nil, errScoreParse
	}
	return raw, nil
}

func scoringPrompt(rubric string, item caseDraft, output, trajectory string) string {
	var builder strings.Builder
	builder.WriteString("评分标准：\n")
	builder.WriteString(rubric)
	builder.WriteString("\n\n用户问题：\n")
	builder.WriteString(item.Query)
	if item.ExpectedAnswer != "" {
		builder.WriteString("\n\n期望回答：\n")
		builder.WriteString(item.ExpectedAnswer)
	}
	if len(item.ExpectedToolTrace) > 0 {
		builder.WriteString("\n\n期望工具轨迹：\n")
		builder.Write(item.ExpectedToolTrace)
	}
	if item.Acceptance != "" {
		builder.WriteString("\n\n验收要点：\n")
		builder.WriteString(item.Acceptance)
	}
	builder.WriteString("\n\n实际回答：\n")
	builder.WriteString(output)
	builder.WriteString("\n\n执行轨迹：\n")
	builder.WriteString(trajectory)
	builder.WriteString("\n\n请只返回一个 JSON 对象，字段为 score（0 到 100 的数字）、passed（布尔）、explanation（字符串）、dimensions（对象数组，元素含 name 和 score）。系统异常不要算进回答质量。")
	return builder.String()
}
