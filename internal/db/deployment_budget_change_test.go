package db

import (
	"encoding/json"
	"testing"
)

func TestDeploymentExecutionDetectsBudgetChanges(t *testing.T) {
	without := Deployment{}
	with := Deployment{Budget: json.RawMessage(`{"type":"limit","max_list_cost":{"amount":"100","currency":"USD"}}`)}
	if sameDeploymentExecution(without, with) || sameDeploymentExecution(with, without) {
		t.Fatal("部署预算变更被判定为未变化")
	}
	if !sameDeploymentExecution(with, with) {
		t.Fatal("相同预算被判定为变化")
	}
}
