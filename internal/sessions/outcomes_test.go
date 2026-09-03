package sessions

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestValidateDefineOutcomePayloadRubric(t *testing.T) {
	// 失败场景先行：rubric 结构校验（text/file 两种 + 缺失字段）
	rejectCases := []struct {
		name    string
		payload map[string]any
	}{
		{"missing description", map[string]any{"rubric": map[string]any{"type": "text", "content": "x"}}},
		{"rubric not object", map[string]any{"description": "d", "rubric": "plain string"}},
		{"text rubric missing content", map[string]any{"description": "d", "rubric": map[string]any{"type": "text"}}},
		{"text rubric empty content", map[string]any{"description": "d", "rubric": map[string]any{"type": "text", "content": "  "}}},
		{"file rubric missing file_id", map[string]any{"description": "d", "rubric": map[string]any{"type": "file"}}},
		{"unknown rubric type", map[string]any{"description": "d", "rubric": map[string]any{"type": "pdf", "content": "x"}}},
	}
	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDefineOutcomePayload(tc.payload); err == nil {
				t.Fatalf("validateDefineOutcomePayload(%v) 应报错", tc.payload)
			}
		})
	}

	// 成功场景：text 和 file 两种 rubric
	acceptCases := []struct {
		name    string
		payload map[string]any
	}{
		{"text rubric", map[string]any{"description": "d", "rubric": map[string]any{"type": "text", "content": "# Rubric"}}},
		{"file rubric", map[string]any{"description": "d", "rubric": map[string]any{"type": "file", "file_id": "file_123"}}},
	}
	for _, tc := range acceptCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDefineOutcomePayload(tc.payload); err != nil {
				t.Fatalf("validateDefineOutcomePayload(%v) 应通过: %v", tc.payload, err)
			}
		})
	}
}

func TestNormalizeInputEventValidatesRubricFile(t *testing.T) {
	// 失败场景先行：file rubric 的文件不存在 → 报错
	payload := map[string]any{
		"type":        "user.define_outcome",
		"description": "d",
		"rubric":      map[string]any{"type": "file", "file_id": "file_missing"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = normalizeInputEvent(db.Session{}, raw, time.Now(), func(workspaceUUID, fileID string) error {
		return errors.New("file not found: " + fileID)
	})
	if err == nil || !strings.Contains(err.Error(), "file_missing") {
		t.Fatalf("file rubric 文件不存在应报错, got %v", err)
	}

	// 成功场景：文件存在 → 通过
	raw, _ = json.Marshal(payload)
	_, _, _, err = normalizeInputEvent(db.Session{}, raw, time.Now(), func(_ string, _ string) error { return nil })
	if err != nil {
		t.Fatalf("文件存在应通过: %v", err)
	}

	// text rubric 不触发文件校验
	textRaw, _ := json.Marshal(map[string]any{
		"type":        "user.define_outcome",
		"description": "d",
		"rubric":      map[string]any{"type": "text", "content": "# Rubric"},
	})
	_, _, _, err = normalizeInputEvent(db.Session{}, textRaw, time.Now(), func(_ string, _ string) error {
		return errors.New("不应调用文件校验")
	})
	if err != nil {
		t.Fatalf("text rubric 不应触发文件校验: %v", err)
	}
}

func TestNormalizeInputEventRejectsNonStringRubricFileID(t *testing.T) {
	// 失败场景先行：file_id 非字符串必须报错（此前断言失败被静默跳过）
	raw, _ := json.Marshal(map[string]any{
		"type":        "user.define_outcome",
		"description": "d",
		"rubric":      map[string]any{"type": "file", "file_id": 123},
	})
	_, _, _, err := normalizeInputEvent(db.Session{}, raw, time.Now(), func(_ string, _ string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "rubric.file_id") {
		t.Fatalf("file_id 非字符串应报错, got %v", err)
	}
}

func TestNormalizeInputEventRejectsFractionalMaxIterations(t *testing.T) {
	// 失败场景先行：max_iterations 必须是整数（3.5 在入口拒绝，业务层不做浮点兜底）
	raw, _ := json.Marshal(map[string]any{
		"type":           "user.define_outcome",
		"description":    "d",
		"rubric":         map[string]any{"type": "text", "content": "# Rubric"},
		"max_iterations": 3.5,
	})
	_, _, _, err := normalizeInputEvent(db.Session{}, raw, time.Now(), nil)
	if err == nil || !strings.Contains(err.Error(), "integer") {
		t.Fatalf("3.5 应报错, got %v", err)
	}
}
