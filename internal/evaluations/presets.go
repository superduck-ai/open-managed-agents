package evaluations

import (
	"encoding/json"
	"errors"
	"time"
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

var omaPresetDatasets []presetDataset
var omaPresetEvaluators []presetEvaluator

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

