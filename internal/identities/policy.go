package identities

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

const (
	pairingCodeTTL = 10 * time.Minute
	codeAlphabet   = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
)

type VaultState string

const (
	VaultActive   VaultState = "active"
	VaultMissing  VaultState = "missing"
	VaultArchived VaultState = "archived"
)

type CredentialInput struct {
	ChatType       string
	SharedMounts   []string
	Availability   map[string]VaultState
	PersonalVault  string
	PersonalActive bool
}

type CredentialSet struct {
	VaultIDs        []string
	PersonalVaultID string
	SharedVaultIDs  []string
}

func EnabledMounts(vaults, disabled []string) []string {
	skip := map[string]struct{}{}
	for _, id := range disabled {
		skip[id] = struct{}{}
	}
	kept := make([]string, 0, len(vaults))
	for _, id := range vaults {
		if id == "" {
			continue
		}
		if _, omit := skip[id]; omit {
			continue
		}
		kept = append(kept, id)
	}
	return kept
}

func PlanCredentials(input CredentialInput) (CredentialSet, error) {
	shared := make([]string, 0, len(input.SharedMounts))
	for _, id := range input.SharedMounts {
		if id == "" || slices.Contains(shared, id) {
			continue
		}
		state := input.Availability[id]
		if state == "" {
			state = VaultMissing
		}
		if state != VaultActive {
			return CredentialSet{}, errCredentialUnavailable
		}
		shared = append(shared, id)
	}
	set := CredentialSet{VaultIDs: append([]string{}, shared...), SharedVaultIDs: shared}
	if input.ChatType != "p2p" {
		return set, nil
	}
	if input.PersonalVault == "" {
		return set, nil
	}
	if !input.PersonalActive {
		return CredentialSet{}, errPersonalVaultUnavailable
	}
	set.PersonalVaultID = input.PersonalVault
	if !slices.Contains(set.VaultIDs, input.PersonalVault) {
		set.VaultIDs = append(set.VaultIDs, input.PersonalVault)
	}
	return set, nil
}

type ConfigState struct {
	Env      map[string]string
	Vaults   []string
	Disabled []string
}

type ConfigPatch struct {
	EnvSet      bool
	EnvClear    bool
	Env         map[string]*string
	VaultsSet   bool
	VaultsClear bool
	Vaults      []string
	DisabledSet bool
	Disabled    []string
}

func MergeConfig(current ConfigState, patch ConfigPatch) (ConfigState, error) {
	next := ConfigState{Env: cloneMap(current.Env), Vaults: append([]string{}, current.Vaults...), Disabled: append([]string{}, current.Disabled...)}
	if patch.EnvClear {
		next.Env = map[string]string{}
	}
	if patch.EnvSet {
		if next.Env == nil {
			next.Env = map[string]string{}
		}
		for key, value := range patch.Env {
			if err := validateEnv(key, value); err != nil {
				return ConfigState{}, err
			}
			if value == nil {
				delete(next.Env, key)
				continue
			}
			next.Env[key] = *value
		}
	}
	if patch.VaultsClear {
		next.Vaults = []string{}
	}
	if patch.VaultsSet {
		next.Vaults = append([]string{}, patch.Vaults...)
	}
	if patch.DisabledSet {
		next.Disabled = append([]string{}, patch.Disabled...)
	}
	return next, nil
}

func ConfigChanged(left, right ConfigState) bool {
	return !mapsEqual(left.Env, right.Env) || !slices.Equal(left.Vaults, right.Vaults) || !slices.Equal(left.Disabled, right.Disabled)
}

type AgentBase struct {
	Version int
	Env     map[string]string
	Vaults  []string
	Tools   []string
}

type SourcedValue struct {
	Name   string `json:"name,omitempty"`
	Value  string `json:"value,omitempty"`
	ID     string `json:"id,omitempty"`
	Source string `json:"source"`
}

type EffectiveConfig struct {
	AgentVersion    int            `json:"agent_version"`
	Revision        int64          `json:"revision"`
	Env             []SourcedValue `json:"env"`
	Vaults          []SourcedValue `json:"vaults"`
	DisabledVaults  []string       `json:"disabled_vaults"`
	Tools           []SourcedValue `json:"tools"`
	AutoMemoryStore SourcedValue   `json:"auto_memory_store"`
}

func CompileEffective(base AgentBase, cfg ConfigState, revision int64, autoMemory bool) EffectiveConfig {
	disabled := map[string]struct{}{}
	for _, id := range cfg.Disabled {
		disabled[id] = struct{}{}
	}
	env := map[string]SourcedValue{}
	for key, value := range base.Env {
		if vaults.PlatformReservedSecretName(key) || secretEnvKey(key) {
			continue
		}
		env[key] = SourcedValue{Name: key, Value: value, Source: "agent"}
	}
	for key, value := range cfg.Env {
		if vaults.PlatformReservedSecretName(key) || secretEnvKey(key) {
			continue
		}
		env[key] = SourcedValue{Name: key, Value: value, Source: "identity_config"}
	}
	names := make([]string, 0, len(env))
	for key := range env {
		names = append(names, key)
	}
	slices.Sort(names)
	effective := EffectiveConfig{AgentVersion: base.Version, Revision: revision, DisabledVaults: append([]string{}, cfg.Disabled...), AutoMemoryStore: SourcedValue{Value: "false", Source: "identity"}}
	if autoMemory {
		effective.AutoMemoryStore.Value = "true"
	}
	for _, key := range names {
		effective.Env = append(effective.Env, env[key])
	}
	seen := map[string]struct{}{}
	for _, id := range base.Vaults {
		if _, skip := disabled[id]; skip || id == "" {
			continue
		}
		seen[id] = struct{}{}
		effective.Vaults = append(effective.Vaults, SourcedValue{ID: id, Source: "agent"})
	}
	for _, id := range cfg.Vaults {
		if _, skip := disabled[id]; skip || id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		effective.Vaults = append(effective.Vaults, SourcedValue{ID: id, Source: "identity_config"})
	}
	for _, tool := range base.Tools {
		effective.Tools = append(effective.Tools, SourcedValue{Name: tool, Source: "agent"})
	}
	return effective
}

type Pairing struct {
	Status       string
	IdentityID   string
	AgentID      string
	AgentVersion *int
	Revision     int64
	CodeHash     string
	CodeExpires  time.Time
}

func SameTarget(pairing Pairing, identityID, agentID string, version *int) bool {
	return pairing.IdentityID == identityID && pairing.AgentID == agentID && sameVersion(pairing.AgentVersion, version)
}

func ActivateWithCode(pairing Pairing, code string, now time.Time, identityID, agentID string, version *int) (Pairing, error) {
	if pairing.Status != "pending" {
		return Pairing{}, errors.New("pairing is not pending")
	}
	if pairing.CodeExpires.IsZero() || !now.Before(pairing.CodeExpires) || HashPairingCode(code) != pairing.CodeHash {
		return Pairing{}, errors.New("pairing code expired")
	}
	pairing.Status = "active"
	pairing.IdentityID = identityID
	pairing.AgentID = agentID
	pairing.AgentVersion = version
	pairing.Revision = 1
	pairing.CodeHash = ""
	pairing.CodeExpires = time.Time{}
	return pairing, nil
}

func ActivateWithID(pairing Pairing, identityID, agentID string, version *int) (Pairing, bool, error) {
	if pairing.Status == "active" && SameTarget(pairing, identityID, agentID, version) {
		return pairing, false, nil
	}
	if pairing.Status == "unbound" {
		return Pairing{}, false, errors.New("unbound pairing cannot be restored")
	}
	if pairing.Status == "active" {
		return Pairing{}, false, errors.New("active pairing target differs")
	}
	if pairing.Status != "pending" {
		return Pairing{}, false, errors.New("pairing is not pending")
	}
	pairing.Status = "active"
	pairing.IdentityID = identityID
	pairing.AgentID = agentID
	pairing.AgentVersion = version
	pairing.Revision = 1
	pairing.CodeHash = ""
	pairing.CodeExpires = time.Time{}
	return pairing, true, nil
}

func Rebind(pairing Pairing, expected int64, identityID, agentID string, version *int) (Pairing, bool, error) {
	if pairing.Status != "active" {
		return Pairing{}, false, errors.New("only an active pairing can be updated")
	}
	if pairing.Revision != expected {
		return Pairing{}, false, errors.New("revision conflict")
	}
	if SameTarget(pairing, identityID, agentID, version) {
		return pairing, false, nil
	}
	pairing.IdentityID = identityID
	pairing.AgentID = agentID
	pairing.AgentVersion = version
	pairing.Revision++
	return pairing, true, nil
}

func Unbind(pairing Pairing, expected int64) (Pairing, bool, error) {
	if pairing.Status == "unbound" {
		return pairing, false, nil
	}
	if pairing.Status != "active" {
		return Pairing{}, false, errors.New("pairing is not active")
	}
	if pairing.Revision != expected {
		return Pairing{}, false, errors.New("revision conflict")
	}
	pairing.Status = "unbound"
	pairing.Revision++
	pairing.CodeHash = ""
	return pairing, true, nil
}

type SessionBinding struct {
	IdentityID      string
	AgentID         string
	BindingRevision int64
	ConfigRevision  int64
	PersonalVaultID string
	ScopeKey        string
}

type EntryState struct {
	IdentityID      string
	AgentID         string
	BindingRevision int64
	ConfigRevision  int64
	PersonalVaultID string
	ScopeKey        string
	IdentityEnabled bool
}

func SessionCurrent(binding SessionBinding, entry EntryState) bool {
	return entry.IdentityEnabled && binding.IdentityID == entry.IdentityID && binding.AgentID == entry.AgentID &&
		binding.BindingRevision == entry.BindingRevision && binding.ConfigRevision == entry.ConfigRevision &&
		binding.PersonalVaultID == entry.PersonalVaultID && binding.ScopeKey == entry.ScopeKey
}

func FilterCurrentSessions(bindings []SessionBinding, entry EntryState) []SessionBinding {
	kept := make([]SessionBinding, 0, len(bindings))
	for _, binding := range bindings {
		if SessionCurrent(binding, entry) {
			kept = append(kept, binding)
		}
	}
	return kept
}

type ToolWait struct {
	SessionID      string
	VaultID        string
	CallID         string
	PendingToolIDs []string
	CompletedIDs   []string
}

type ToolResume struct {
	SessionID     string
	VaultID       string
	CallID        string
	ReplayToolIDs []string
}

func ResumeOriginalCall(wait ToolWait) ToolResume {
	resume := ToolResume{SessionID: wait.SessionID, VaultID: wait.VaultID, CallID: wait.CallID}
	for _, id := range wait.PendingToolIDs {
		if id == wait.CallID || slices.Contains(wait.CompletedIDs, id) {
			continue
		}
		resume.ReplayToolIDs = append(resume.ReplayToolIDs, id)
	}
	return resume
}

func UnknownDeliveryResend() bool { return false }

type CleanupPreview struct {
	RunningSessions []string
	SharedVaultIDs  []string
	PersonalVaults  int
}

type CleanupOutcome struct {
	Completed              bool     `json:"completed"`
	RunningSessions        []string `json:"running_sessions,omitempty"`
	ArchivedConfigs        int      `json:"archived_configs"`
	UnboundPairings        int      `json:"unbound_pairings"`
	ClosedEntries          int      `json:"closed_entries"`
	RetainedSharedVaults   []string `json:"retained_shared_vaults"`
	RetainedPersonalVaults int      `json:"retained_personal_vaults"`
	PersonalVaultsDeleted  bool     `json:"personal_vaults_deleted"`
	Failures               []string `json:"failures,omitempty"`
}

func CleanupBlocked(preview CleanupPreview) bool {
	return len(preview.RunningSessions) > 0
}

func FinishCleanup(outcome CleanupOutcome) CleanupOutcome {
	outcome.Completed = len(outcome.Failures) == 0
	if outcome.RetainedSharedVaults == nil {
		outcome.RetainedSharedVaults = []string{}
	}
	return outcome
}

func MergeCleanupOutcome(prior, next CleanupOutcome) CleanupOutcome {
	seen := map[string]struct{}{}
	retained := make([]string, 0, len(prior.RetainedSharedVaults)+len(next.RetainedSharedVaults))
	for _, id := range append(append([]string{}, prior.RetainedSharedVaults...), next.RetainedSharedVaults...) {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		retained = append(retained, id)
	}
	next.RetainedSharedVaults = retained
	next.ArchivedConfigs = max(next.ArchivedConfigs, prior.ArchivedConfigs)
	next.UnboundPairings = max(next.UnboundPairings, prior.UnboundPairings)
	next.ClosedEntries = max(next.ClosedEntries, prior.ClosedEntries)
	next.RetainedPersonalVaults = max(next.RetainedPersonalVaults, prior.RetainedPersonalVaults)
	if len(next.RunningSessions) == 0 {
		next.RunningSessions = prior.RunningSessions
	}
	return FinishCleanup(next)
}

func ScopeKind(chatType string) string {
	if chatType == "p2p" {
		return "direct"
	}
	return "room"
}

func AllowlistGrantsIdentity() bool { return false }

func HashPairingCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func NewPairingCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var code [8]byte
	for i, b := range buf {
		code[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}
	return string(code[:]), nil
}

func validateEnv(key string, value *string) error {
	if strings.TrimSpace(key) == "" || key != strings.TrimSpace(key) {
		return errors.New("env key is invalid")
	}
	if vaults.PlatformReservedSecretName(key) || secretEnvKey(key) {
		return errors.New("env cannot store a secret")
	}
	if value != nil && secretEnvValue(*value) {
		return errors.New("env cannot store a secret")
	}
	return nil
}

func secretEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, token := range []string{"SECRET", "PASSWORD", "TOKEN", "API_KEY", "CREDENTIAL", "PRIVATE_KEY"} {
		if strings.Contains(upper, token) {
			return true
		}
	}
	return false
}

func secretEnvValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "sk-") || strings.HasPrefix(trimmed, "xox")
}

func sameVersion(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
