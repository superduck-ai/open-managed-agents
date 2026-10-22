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
	presetEvaluatorNote  = "OMA demo preset. Not a QCA evaluator."
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

func buildPresetEvaluators() []presetEvaluator {
	evaluators := make([]presetEvaluator, 0, presetEvaluatorCount)
	for index := 1; index <= presetEvaluatorCount; index++ {
		evaluators = append(evaluators, buildPresetEvaluator(index))
	}
	return evaluators
}

func buildPresetEvaluator(index int) presetEvaluator {
	kind := evaluatorTypeAgent
	model := ""
	agentID := fmt.Sprintf("agent_oma_demo_%02d", index)
	prompt := ""
	variables := []string{"input", "output"}
	if index <= 4 {
		kind = evaluatorTypeLLM
		model = "oma-demo"
		agentID = ""
		variables = presetLLMVariables(index)
		prompt = presetLLMPrompt(variables)
	}
	return presetEvaluator{
		ID:             fmt.Sprintf("evalev_oma_demo_%02d", index),
		Name:           fmt.Sprintf("OMA demo evaluator %02d", index),
		Description:    presetEvaluatorNote,
		Archived:       index == presetEvaluatorCount,
		VersionID:      fmt.Sprintf("eev_oma_demo_%02d_v1", index),
		EvaluatorType:  kind,
		Model:          model,
		AgentID:        agentID,
		PromptTemplate: prompt,
		Variables:      variables,
	}
}

func presetLLMVariables(index int) []string {
	switch index {
	case 2:
		return []string{"input", "output"}
	case 3:
		return []string{"output", "checkpoints"}
	case 4:
		return []string{"agent_trajectory", "invocation_result"}
	default:
		return []string{"input"}
	}
}

func presetLLMPrompt(variables []string) string {
	parts := make([]string, 0, len(variables))
	for _, name := range variables {
		parts = append(parts, "{{"+name+"}}")
	}
	return "OMA demo scorer. Score the answer from 0 to 1. " + strings.Join(parts, " ")
}
