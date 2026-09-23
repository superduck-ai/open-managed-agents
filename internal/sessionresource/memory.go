package sessionresource

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gosimple/slug"

	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
)

const (
	MemoryStoreType            = sessioncontract.MemoryStoreResourceType
	MaxMemoryStores            = sessioncontract.MaxMemoryStores
	MaxMemoryInstructionsRunes = sessioncontract.MaxMemoryInstructionsRunes
	MemoryMountRoot            = "/mnt/memory"
)

// MemoryAccess describes the access granted by a memory store attachment.
// External values must be validated by ParseMemoryAccess or NormalizeMemoryAccess.
type MemoryAccess string

const (
	MemoryAccessReadWrite MemoryAccess = "read_write"
	MemoryAccessReadOnly  MemoryAccess = "read_only"
)

var (
	ErrMemoryStoreClientIdentity      = errors.New("memory store mount_path, name, and description are assigned by the server")
	ErrMemoryStoreAccess              = errors.New("access must be read_write or read_only")
	ErrMemoryStoreInstructionsTooLong = errors.New("instructions must be at most 500 characters")
	ErrMemoryStoreLimit               = fmt.Errorf("at most %d memory stores are allowed", MaxMemoryStores)
	ErrMemoryStoreDuplicate           = errors.New("memory_store_id must be unique")
)

// MemorySnapshot is the server-authored attach record stored on session_resources.
type MemorySnapshot struct {
	MemoryStoreID string       `json:"memory_store_id"`
	Access        MemoryAccess `json:"access"`
	Instructions  string       `json:"instructions"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	MountPath     string       `json:"mount_path"`
}

// MemoryAttachSet tracks store IDs and slugs already claimed in one Session.
type MemoryAttachSet struct {
	ids   map[string]struct{}
	slugs map[string]struct{}
}

func NewMemoryAttachSet() *MemoryAttachSet {
	return &MemoryAttachSet{
		ids:   make(map[string]struct{}),
		slugs: make(map[string]struct{}),
	}
}

func RejectClientMemoryIdentityFields(mountPath, name, description json.RawMessage) error {
	if len(mountPath) > 0 || len(name) > 0 || len(description) > 0 {
		return ErrMemoryStoreClientIdentity
	}
	return nil
}

// NormalizeMemoryAccess is the single authority on attach access values. An
// empty value means the documented default; anything else must name one of the
// two known modes, because a malformed value must never widen the mount.
func NormalizeMemoryAccess(value string) (MemoryAccess, error) {
	if value == "" {
		return MemoryAccessReadWrite, nil
	}
	access := MemoryAccess(value)
	if access != MemoryAccessReadWrite && access != MemoryAccessReadOnly {
		return "", ErrMemoryStoreAccess
	}
	return access, nil
}

func ParseMemoryAccess(raw json.RawMessage) (MemoryAccess, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return MemoryAccessReadWrite, nil
	}
	value, err := requiredString(raw, "access")
	if err != nil {
		return "", err
	}
	return NormalizeMemoryAccess(value)
}

func ParseMemoryInstructions(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("instructions must be a string")
	}
	if utf8.RuneCountInString(value) > MaxMemoryInstructionsRunes {
		return "", ErrMemoryStoreInstructionsTooLong
	}
	return value, nil
}

func SlugifyMemoryName(name, fallbackExternalID string) string {
	if token := slug.Make(name); token != "" {
		return token
	}
	if token := slug.Make(fallbackExternalID); token != "" {
		return token
	}
	return "store"
}

func MemoryMountPath(slug string) string {
	return MemoryMountRoot + "/" + slug
}

func MemorySlugFromMountPath(mountPath string) string {
	if !strings.HasPrefix(mountPath, MemoryMountRoot+"/") {
		return ""
	}
	return strings.TrimPrefix(mountPath, MemoryMountRoot+"/")
}

func (s *MemoryAttachSet) Observe(storeID, slug string) {
	if s.ids == nil {
		s.ids = make(map[string]struct{})
	}
	if s.slugs == nil {
		s.slugs = make(map[string]struct{})
	}
	if storeID != "" {
		s.ids[storeID] = struct{}{}
	}
	if slug != "" {
		s.slugs[slug] = struct{}{}
	}
}

func ObserveStoredMemoryResource(set *MemoryAttachSet, resourceType string, payload json.RawMessage) {
	if set == nil || resourceType != MemoryStoreType {
		return
	}
	var body struct {
		MemoryStoreID string `json:"memory_store_id"`
		MountPath     string `json:"mount_path"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return
	}
	set.Observe(body.MemoryStoreID, MemorySlugFromMountPath(body.MountPath))
}

func (s *MemoryAttachSet) Claim(storeID string) error {
	if s.ids == nil {
		s.ids = make(map[string]struct{})
	}
	if _, exists := s.ids[storeID]; exists {
		return ErrMemoryStoreDuplicate
	}
	if len(s.ids) >= MaxMemoryStores {
		return ErrMemoryStoreLimit
	}
	s.ids[storeID] = struct{}{}
	return nil
}

func (s *MemoryAttachSet) Add(storeID, name, fallbackExternalID string) (string, error) {
	if err := s.Claim(storeID); err != nil {
		return "", err
	}
	return s.claimSlug(SlugifyMemoryName(name, fallbackExternalID)), nil
}

func (s *MemoryAttachSet) claimSlug(base string) string {
	if s.slugs == nil {
		s.slugs = make(map[string]struct{})
	}
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, exists := s.slugs[candidate]; !exists {
			s.slugs[candidate] = struct{}{}
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

// MemorySnapshotPayload is the Session resource serialization boundary.
type MemorySnapshotPayload struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type"`
	MemorySnapshot
}

func (s MemorySnapshot) Payload(resourceID string) MemorySnapshotPayload {
	return MemorySnapshotPayload{ID: resourceID, Type: MemoryStoreType, MemorySnapshot: s}
}

func SnapshotMemoryStore(storeID string, access MemoryAccess, instructions, name, description, slug string) MemorySnapshot {
	return MemorySnapshot{
		MemoryStoreID: storeID,
		Access:        access,
		Instructions:  instructions,
		Name:          name,
		Description:   description,
		MountPath:     MemoryMountPath(slug),
	}
}
