package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
)

type storedCompanion struct {
	kind string
	rel  string
	ref  sessionCaseFileRef
}

func (h *Handler) storeImportCompanions(ctx context.Context, workspaceUUID string, bundle importBundle) ([]normalizedCase, error) {
	if len(bundle.companions) == 0 {
		return bundle.cases, nil
	}
	if h.files == nil {
		return nil, importCompanionsUnavailable()
	}
	stored := make([]storedCompanion, 0, len(bundle.companions))
	for _, companion := range bundle.companions {
		ref, err := h.files.PutCaseFile(ctx, workspaceUUID, companion.rel, companion.body)
		if err != nil {
			return nil, mapImportFileError(err)
		}
		ref.Name = companion.rel
		stored = append(stored, storedCompanion{kind: companion.kind, rel: companion.rel, ref: ref})
	}
	return assignStoredCompanions(bundle.cases, stored)
}

func mapImportFileError(err error) error {
	switch {
	case errors.Is(err, errCaseFileTooLarge):
		return importCompanionTooLarge()
	case errors.Is(err, errCaseFileStorage):
		return importCompanionStorage()
	default:
		return importCompanionFailed(err)
	}
}

func assignStoredCompanions(cases []normalizedCase, stored []storedCompanion) ([]normalizedCase, error) {
	used := make([]bool, len(stored))
	next := append([]normalizedCase(nil), cases...)
	for index := range next {
		inputFiles, err := mergeCaseFiles(next[index].InputFiles, "input", stored, used)
		if err != nil {
			return nil, err
		}
		referenceFiles, err := mergeCaseFiles(next[index].ReferenceFiles, "reference", stored, used)
		if err != nil {
			return nil, err
		}
		next[index].InputFiles = inputFiles
		next[index].ReferenceFiles = referenceFiles
	}
	if err := appendUnusedCompanions(next, stored, used); err != nil {
		return nil, err
	}
	return next, nil
}

func mergeCaseFiles(raw json.RawMessage, kind string, stored []storedCompanion, used []bool) (json.RawMessage, error) {
	refs, err := decodeCaseFileRefs(raw)
	if err != nil {
		return nil, fileRefsInvalid(kind)
	}
	for index, ref := range refs {
		if ref.Name == "" && ref.FileID == "" {
			continue
		}
		match := matchCompanion(kind, ref.Name, stored)
		if match >= 0 {
			refs[index].FileID = stored[match].ref.FileID
			if strings.TrimSpace(refs[index].Name) == "" {
				refs[index].Name = stored[match].rel
			}
			used[match] = true
			continue
		}
		if strings.TrimSpace(ref.FileID) == "" {
			return nil, importCompanionUnresolved()
		}
	}
	return marshalSessionCaseFiles(compactCaseFileRefs(refs))
}

func appendUnusedCompanions(cases []normalizedCase, stored []storedCompanion, used []bool) error {
	for index, companion := range stored {
		if used[index] {
			continue
		}
		for _, target := range companionTargets(cases, companion.rel) {
			if err := appendCaseFile(&cases[target], companion); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendCaseFile(item *normalizedCase, companion storedCompanion) error {
	raw := &item.InputFiles
	if companion.kind == "reference" {
		raw = &item.ReferenceFiles
	}
	refs, err := decodeCaseFileRefs(*raw)
	if err != nil {
		return fileRefsInvalid(companion.kind)
	}
	refs = append(refs, companion.ref)
	encoded, err := marshalSessionCaseFiles(compactCaseFileRefs(refs))
	if err != nil {
		return err
	}
	*raw = encoded
	return nil
}

func companionTargets(cases []normalizedCase, rel string) []int {
	dir := path.Dir(strings.ReplaceAll(rel, "\\", "/"))
	if dir != "." && dir != "" {
		first := strings.Split(dir, "/")[0]
		matched := make([]int, 0)
		for index, item := range cases {
			if item.Name == first {
				matched = append(matched, index)
			}
		}
		if len(matched) > 0 {
			return matched
		}
	}
	indexes := make([]int, len(cases))
	for index := range cases {
		indexes[index] = index
	}
	return indexes
}

func matchCompanion(kind, name string, stored []storedCompanion) int {
	cleaned := normalizeCompanionKey(name)
	if cleaned == "" {
		return -1
	}
	exact := -1
	baseMatches := make([]int, 0)
	base := normalizeCompanionKey(path.Base(cleaned))
	for index, item := range stored {
		if item.kind != kind {
			continue
		}
		rel := normalizeCompanionKey(item.rel)
		if rel == cleaned {
			exact = index
		}
		if base != "" && normalizeCompanionKey(path.Base(rel)) == base {
			baseMatches = append(baseMatches, index)
		}
	}
	if exact >= 0 {
		return exact
	}
	if len(baseMatches) == 1 {
		return baseMatches[0]
	}
	return -1
}

func normalizeCompanionKey(value string) string {
	slash := strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/")
	return strings.ToLower(slash)
}

func decodeCaseFileRefs(raw json.RawMessage) ([]sessionCaseFileRef, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	refs := make([]sessionCaseFileRef, 0, len(items))
	for _, item := range items {
		var ref sessionCaseFileRef
		if err := json.Unmarshal(item, &ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func compactCaseFileRefs(refs []sessionCaseFileRef) []sessionCaseFileRef {
	compact := make([]sessionCaseFileRef, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.Name) == "" && strings.TrimSpace(ref.FileID) == "" {
			continue
		}
		compact = append(compact, ref)
	}
	return compact
}

func fileRefsInvalid(kind string) error {
	if kind == "reference" {
		return referenceFilesInvalid()
	}
	return inputFilesInvalid()
}
