package evaluations

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	presetDatasetCount   = 19
	presetEvaluatorCount = 11
	presetDemoNote       = "OMA demo preset. Not a QCA dataset."
	presetEvaluatorNote  = "OMA-authored preset. Not a QCA evaluator."
)

var (
	errPresetNotFound = errors.New("preset not found")
	errPresetArchived = errors.New("preset archived")
	presetPublishedAt = time.Date(2026, time.October, 22, 0, 0, 0, 0, time.UTC)
)

type presetCase struct {
	ID             string
	Name           string
	Description    string
	Turns          []string
	Checkpoints    json.RawMessage
	InputFiles     json.RawMessage
	ReferenceFiles json.RawMessage
}

type presetDatasetVersion struct {
	ID      string
	Version int
	Cases   []presetCase
}

type presetDataset struct {
	ID          string
	Name        string
	Description string
	Archived    bool
	Versions    []presetDatasetVersion
}

type presetEvaluator struct {
	ID             string
	Name           string
	Description    string
	Archived       bool
	VersionID      string
	EvaluatorType  string
	Model          string
	AgentID        string
	PromptTemplate string
	Variables      []string
}

var omaPresetDatasets = buildPresetDatasets()
var omaPresetEvaluators = buildPresetEvaluators()

func presetDatasets() []presetDataset {
	return omaPresetDatasets
}

func presetEvaluators() []presetEvaluator {
	return omaPresetEvaluators
}

func presetDatasetByID(id string) (presetDataset, error) {
	for _, dataset := range omaPresetDatasets {
		if dataset.ID == id {
			return dataset, nil
		}
	}
	return presetDataset{}, errPresetNotFound
}

func lookupPresetDatasetVersion(versionID string) (presetDataset, presetDatasetVersion, error) {
	for _, dataset := range omaPresetDatasets {
		for _, version := range dataset.Versions {
			if version.ID != versionID {
				continue
			}
			if dataset.Archived {
				return presetDataset{}, presetDatasetVersion{}, errPresetArchived
			}
			return dataset, version, nil
		}
	}
	return presetDataset{}, presetDatasetVersion{}, errPresetNotFound
}

func presetEvaluatorVersion(versionID string) (presetEvaluator, error) {
	for _, evaluator := range omaPresetEvaluators {
		if evaluator.VersionID != versionID {
			continue
		}
		if evaluator.Archived {
			return presetEvaluator{}, errPresetArchived
		}
		return evaluator, nil
	}
	return presetEvaluator{}, errPresetNotFound
}

func presetDatasetVersionOwned(dataset presetDataset, versionID string) (presetDatasetVersion, error) {
	if versionID == "" {
		return dataset.Versions[len(dataset.Versions)-1], nil
	}
	for _, version := range dataset.Versions {
		if version.ID == versionID {
			return version, nil
		}
	}
	return presetDatasetVersion{}, errPresetNotFound
}

func buildPresetDatasets() []presetDataset {
	datasets := make([]presetDataset, 0, presetDatasetCount)
	for index := 1; index <= presetDatasetCount; index++ {
		dataset := presetDataset{
			ID:          fmt.Sprintf("evalds_oma_demo_%02d", index),
			Name:        fmt.Sprintf("OMA demo dataset %02d", index),
			Description: presetDemoNote,
			Archived:    index == presetDatasetCount,
			Versions:    []presetDatasetVersion{presetVersion(index, 1)},
		}
		if index == 3 {
			dataset.Versions = append(dataset.Versions, presetVersion(index, 2))
		}
		datasets = append(datasets, dataset)
	}
	return datasets
}

func presetVersion(datasetIndex, version int) presetDatasetVersion {
	cases := []presetCase{presetDemoCase(datasetIndex, version, 1)}
	if version == 2 {
		cases = append(cases, presetDemoCase(datasetIndex, version, 2))
	}
	return presetDatasetVersion{
		ID:      fmt.Sprintf("edv_oma_demo_%02d_v%d", datasetIndex, version),
		Version: version,
		Cases:   cases,
	}
}

func presetDemoCase(datasetIndex, version, position int) presetCase {
	item := presetCase{
		ID:          fmt.Sprintf("evalcase_oma_demo_%02d_v%d_%02d", datasetIndex, version, position),
		Name:        fmt.Sprintf("OMA demo case %02d", position),
		Description: presetDemoNote,
		Turns: []string{
			fmt.Sprintf("OMA demo case %d on dataset %d version %d. Reply with the word ready.", position, datasetIndex, version),
		},
	}
	if datasetIndex == 4 && version == 1 && position == 1 {
		item.Checkpoints = json.RawMessage(`["ready"]`)
	}
	if datasetIndex == 5 && version == 1 && position == 1 {
		item.InputFiles = json.RawMessage(`[{"name":"oma-demo.txt"}]`)
		item.ReferenceFiles = json.RawMessage(`[{"name":"oma-demo-reference.txt"}]`)
	}
	return item
}

type presetAgentSpec struct {
	Slug      string
	Name      string
	Focus     string
	Variables []string
}

func buildPresetEvaluators() []presetEvaluator {
	specs := presetAgentSpecs()
	evaluators := make([]presetEvaluator, 0, len(specs))
	for _, spec := range specs {
		variables := append([]string(nil), spec.Variables...)
		evaluators = append(evaluators, presetEvaluator{
			ID:             "evalev_oma_" + spec.Slug,
			Name:           spec.Name,
			Description:    presetEvaluatorNote + " " + spec.Focus,
			VersionID:      "eev_oma_" + spec.Slug + "_v1",
			EvaluatorType:  evaluatorTypeAgent,
			PromptTemplate: presetAgentPrompt(spec.Focus, variables),
			Variables:      variables,
		})
	}
	return evaluators
}

func presetAgentSpecs() []presetAgentSpec {
	return []presetAgentSpec{
		{Slug: "latency", Name: "时延效率", Focus: "Score time efficiency from how long the run took and whether it stalled. A completed run that finishes promptly scores higher. A timeout or a long idle stretch scores lower. Do not judge whether the answer is correct.", Variables: []string{"agent_trajectory", "invocation_result"}},
		{Slug: "invocation_stability", Name: "调用稳定性", Focus: "Score invocation stability from the final status. A normal success scores high. An error, a crash, or a timeout scores low. Do not grade the wording of the answer.", Variables: []string{"invocation_result"}},
		{Slug: "tool_reliability", Name: "工具调用可靠性", Focus: "Score tool-call reliability. Count tool samples that returned a usable result against tool samples that failed. A higher success rate scores higher. If the trajectory has no tool samples, say so and score 0.", Variables: []string{"agent_trajectory", "invocation_result"}},
		{Slug: "hallucination", Name: "幻觉检测", Focus: "Score hallucination by comparing claims in the answer with evidence in the trajectory and artifacts. Supported claims score high. Invented facts, files, or tool results score low. Do not grade writing style.", Variables: []string{"output", "agent_trajectory"}},
		{Slug: "task_completion", Name: "任务完成度", Focus: "Score task completion: whether the required actions and artifacts were produced. Do not grade whether the content is factually correct. Missing required actions score low.", Variables: []string{"input", "checkpoints", "agent_trajectory"}},
		{Slug: "tool_arguments", Name: "工具参数正确性", Focus: "Score tool argument correctness. For each tool call, check whether the arguments match the task. Arguments that fit the task score high. Wrong names, types, or values score low.", Variables: []string{"input", "agent_trajectory"}},
		{Slug: "tool_selection", Name: "工具选择质量", Focus: "Score tool selection. The tools that were called should be the ones the task needed. Relevant tools score high. Missing needed tools or unrelated tools score low.", Variables: []string{"input", "agent_trajectory"}},
		{Slug: "checkpoint_correctness", Name: "检查点正确性", Focus: "Score the answer against the checkpoints. Met checkpoints score high. Missed checkpoints score low. Treat the checkpoints field as the requirement list.", Variables: []string{"output", "checkpoints"}},
		{Slug: "trajectory_quality", Name: "轨迹质量", Focus: "Score trajectory quality. A strong run plans, observes results, acts, verifies, and then converges. Loops, skipped observation, or stopping before verification score low.", Variables: []string{"agent_trajectory"}},
		{Slug: "answer_evidence", Name: "回答证据支持", Focus: "Score whether the answer is supported by evidence in the trajectory or artifacts. This is not a correctness grade against checkpoints. Unsupported statements score low.", Variables: []string{"output", "agent_trajectory"}},
		{Slug: "step_efficiency", Name: "步骤效率", Focus: "Score step efficiency. Prefer the shortest trajectory that still finishes the task. Repeated steps and unused detours score low. Do not grade answer correctness.", Variables: []string{"input", "agent_trajectory"}},
	}
}

func presetAgentPrompt(focus string, variables []string) string {
	fields := make([]string, 0, len(variables))
	for _, name := range variables {
		fields = append(fields, "{{"+name+"}}")
	}
	return strings.Join([]string{
		"You are scoring one OMA evaluation dimension. This rubric is written for OMA.",
		"Read /data/session/result.json and the artifacts it names.",
		focus,
		"Session fields: " + strings.Join(fields, " "),
		"Write /output/score.json as JSON with score, explanation, and details.",
		"score is a number from 0 to 1. Use 1 only when this dimension is fully met.",
	}, "\n")
}
