package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeModelRejectsInvalidObjectFields(t *testing.T) {
	testCases := []struct {
		name      string
		raw       string
		wantError string
	}{
		{
			name:      "missing id",
			raw:       `{"speed":"standard"}`,
			wantError: "model.id is required",
		},
		{
			name:      "empty id",
			raw:       `{"id":""}`,
			wantError: "model.id must be a non-empty string",
		},
		{
			name:      "invalid speed",
			raw:       `{"id":"claude-sonnet-4-6","speed":"slow"}`,
			wantError: "model.speed must be standard or fast",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := normalizeModel(json.RawMessage(testCase.raw))
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("normalizeModel() error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}

func TestNormalizeModelPreservesRealID(t *testing.T) {
	testCases := []struct {
		name string
		raw  string
		want normalizedAgentModel
	}{
		{
			name: "string model uses standard speed",
			raw:  `"kimi-k2.5"`,
			want: normalizedAgentModel{ID: "kimi-k2.5", Speed: "standard"},
		},
		{
			name: "object model preserves fast speed",
			raw:  `{"id":"kimi-k2.5","speed":"fast"}`,
			want: normalizedAgentModel{ID: "kimi-k2.5", Speed: "fast"},
		},
		{
			name: "string model keeps surrounding whitespace",
			raw:  `" kimi-k2.5 "`,
			want: normalizedAgentModel{ID: " kimi-k2.5 ", Speed: "standard"},
		},
		{
			name: "object model keeps surrounding whitespace",
			raw:  `{"id":" kimi-k2.5 ","speed":"fast"}`,
			want: normalizedAgentModel{ID: " kimi-k2.5 ", Speed: "fast"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := normalizeModel(json.RawMessage(testCase.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != testCase.want.ID || got.Speed != testCase.want.Speed || !effortEqual(got.Effort, testCase.want.Effort) {
				t.Fatalf("normalizeModel() = %#v, want %#v", got, testCase.want)
			}
		})
	}
}

// effortEqual 比较两个 effort 的 JSON 值（json.RawMessage 无法 == 比较）。
func effortEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return string(a) == string(b)
}

func TestNormalizeModelEffort(t *testing.T) {
	// 失败场景先行：非法 effort 值必须报错（此前静默丢弃）
	rejectCases := []struct {
		name string
		raw  string
	}{
		{"invalid level", `{"id":"claude-opus-4-8","effort":"ultra"}`},
		{"empty object", `{"id":"claude-opus-4-8","effort":{"type":""}}`},
		{"non string object", `{"id":"claude-opus-4-8","effort":{"level":"high"}}`},
	}
	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := normalizeModel(json.RawMessage(tc.raw)); err == nil {
				t.Fatalf("normalizeModel(%s) 应报错", tc.raw)
			}
		})
	}

	// 成功场景：字符串与对象两种形式归一化为 {"type": ...}
	acceptCases := []struct {
		name string
		raw  string
		want string
	}{
		{"string effort", `{"id":"claude-opus-4-8","effort":"high"}`, `{"type":"high"}`},
		{"object effort", `{"id":"claude-opus-4-8","effort":{"type":"max"}}`, `{"type":"max"}`},
		{"low effort", `{"id":"claude-opus-4-8","effort":"low"}`, `{"type":"low"}`},
		{"medium effort", `{"id":"claude-opus-4-8","effort":"medium"}`, `{"type":"medium"}`},
		{"trimmed effort", `{"id":"claude-opus-4-8","effort":"  high  "}`, `{"type":"high"}`},
		{"xhigh effort", `{"id":"claude-opus-4-8","effort":{"type":"xhigh"}}`, `{"type":"xhigh"}`},
		{"null effort treated as omitted", `{"id":"claude-opus-4-8","effort":null}`, ``},
	}
	for _, tc := range acceptCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeModel(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("normalizeModel(%s) 应成功: %v", tc.raw, err)
			}
			if string(got.Effort) != tc.want {
				t.Fatalf("normalizeModel(%s).Effort = %s, want %s", tc.raw, got.Effort, tc.want)
			}
		})
	}
}

func TestNormalizeModelNoEffortDefaultsEmpty(t *testing.T) {
	// 省略 effort 时 Effort 保持空（响应省略该字段，对齐官方回填语义）
	got, err := normalizeModel(json.RawMessage(`{"id":"claude-opus-4-8"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Effort) != 0 {
		t.Fatalf("省略 effort 应保持空, got %s", got.Effort)
	}
}
