package identities

import (
	"context"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type Store interface {
	CreateBusinessIdentity(context.Context, db.InsertIdentityParams) (db.IdentityRecord, error)
	FindBusinessIdentity(context.Context, string, string) (db.IdentityRecord, error)
	FindBusinessIdentityByUUID(context.Context, string, string) (db.IdentityRecord, error)
	ListBusinessIdentities(context.Context, db.ListIdentityParams) ([]db.IdentityRecord, error)
	UpdateBusinessIdentity(context.Context, db.UpdateIdentityParams) error
	SetBusinessIdentityEnabled(context.Context, string, string, bool) error
	DeleteBusinessIdentity(context.Context, string, string) error
	CreateIdentityConfig(context.Context, db.InsertIdentityConfigParams) (db.IdentityConfigRecord, error)
	FindActiveIdentityConfig(context.Context, string, string, string) (db.IdentityConfigRecord, error)
	ListIdentityConfigs(context.Context, string, string, string, string, int) ([]db.IdentityConfigRecord, error)
	ListCleanupIdentityConfigs(context.Context, string, string, string, int) ([]db.IdentityConfigRecord, error)
	CleanupEffects(context.Context, string, string) (db.CleanupEffectCounts, error)
	UpdateIdentityConfig(context.Context, db.UpdateIdentityConfigParams) error
	ArchiveIdentityConfigs(context.Context, string, string) (int64, error)
	FindExecutionContext(context.Context, string, string) (db.ExecutionContextRecord, error)
	SetChannelConnectionStatus(context.Context, string, string, string) error
	CloseFixedIdentityEntries(context.Context, string, string) (int64, error)
	CreateScopePairing(context.Context, db.InsertScopePairingParams) (db.ScopePairingRecord, error)
	FindScopePairing(context.Context, string, string) (db.ScopePairingRecord, error)
	FindOpenScopePairing(context.Context, string, string, string, string) (db.ScopePairingRecord, error)
	FindPendingScopePairingByCode(context.Context, string, string, time.Time) (db.ScopePairingRecord, error)
	ListScopePairings(context.Context, db.ListScopePairingParams) ([]db.ScopePairingRecord, error)
	ActivateScopePairingByCode(context.Context, db.ActivateScopePairingParams) error
	ActivateScopePairingByID(context.Context, db.ActivateScopePairingParams) error
	RebindScopePairing(context.Context, db.RebindScopePairingParams) error
	UnbindScopePairing(context.Context, string, string, int64) error
	UnbindIdentityPairings(context.Context, string, string) (int64, error)
	FindPersonalIdentityByActor(context.Context, string, string, string, string, string) (db.IdentityRecord, error)
	IsPersonalIdentity(context.Context, string, string) (bool, error)
	ClaimPersonalIdentityByActor(context.Context, string, string, string, string, string, string) error
	FindSessionIdentity(context.Context, string, string) (db.SessionIdentityRecord, error)
	ListIdentitySessions(context.Context, string, string, int) ([]db.SessionIdentityRecord, error)
	BusinessIdentityStats(context.Context, string, string, *time.Time) (db.IdentityStatsRecord, error)
	ListBusinessIdentityAgents(context.Context, string, string, *time.Time, int) ([]db.IdentityAgentUsageRecord, error)
	ListRunningIdentitySessions(context.Context, string, string) ([]string, error)
	ListAgentIdentityBindings(context.Context, string, string) ([]db.AgentBindingRecord, error)
	IsPersonalChannelVault(context.Context, string, string) (bool, error)
	CreateIdentityCleanupRun(context.Context, db.InsertCleanupRunParams) (db.CleanupRunRecord, error)
	SaveIdentityCleanupRun(context.Context, string, string, string, string) error
	FindIdentityCleanupRun(context.Context, string, string) (db.CleanupRunRecord, error)
	GetVault(context.Context, string, string) (db.Vault, error)
	GetChannelAgentEnvironment(context.Context, string, string, string) (string, error)
	GetAgent(context.Context, string, string) (db.Agent, error)
	GetAgentVersion(context.Context, string, string, int) (db.Agent, error)
	GetSession(context.Context, string, string) (db.Session, bool, error)
}
