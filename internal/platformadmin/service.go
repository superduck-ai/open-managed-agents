package platformadmin

import (
	"context"
	"database/sql"
	"errors"
	"github.com/riverqueue/river"
	"log/slog"
	"strings"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type Service struct {
	delivery *river.Client[*sql.Tx]
	db       *db.DB
	logger   *slog.Logger
}

func New(database *db.DB, logger *slog.Logger) *Service {
	return &Service{db: database, logger: logging.LoggerOrDefault(logger)}
}

type Actor struct {
	AccountUUID string
}

type Access struct {
	Role        *string  `json:"role"`
	Permissions []string `json:"permissions"`
}

func Permissions(granted bool) Access {
	a := Access{Permissions: []string{}}
	if granted {
		a.Role = new("super_admin")
		a.Permissions = []string{"platform:access", "platform:organizations:read", "platform:organizations:write", "platform:organization-admins:manage", "platform:administrators:manage"}
	}
	return a
}

func (s *Service) Authorize(ctx context.Context, accountUUID string) (db.PlatformAdminAccount, error) {
	account, err := s.db.GetPlatformAdminAccount(ctx, accountUUID)
	if errors.Is(err, db.ErrNotFound) {
		return account, ErrDenied
	}
	if err != nil {
		return account, err
	}
	if !account.Granted {
		return account, ErrDenied
	}
	return account, nil
}

func (s *Service) mutate(ctx context.Context, actor Actor, fn func(*db.PlatformAdminTx) error) error {
	return s.db.WithPlatformAdminTx(ctx, func(tx *db.PlatformAdminTx) error {
		if err := lockActor(ctx, tx, actor.AccountUUID); err != nil {
			return err
		}
		return fn(tx)
	})
}
func lockActor(ctx context.Context, tx *db.PlatformAdminTx, accountUUID string) error {
	granted, err := tx.LockGrant(ctx, accountUUID)
	if errors.Is(err, db.ErrNotFound) {
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if !granted {
		return ErrDenied
	}
	return nil
}

func (s *Service) SetAdministrator(ctx context.Context, accountUUID, operator string, _ string, granted bool) error {
	return s.setAdministrator(ctx, accountUUID, strings.TrimSpace(operator), granted, "")
}

func (s *Service) ChangeAdministrator(ctx context.Context, actor Actor, accountUUID string, granted bool) error {
	if actor.AccountUUID == "" {
		return ErrDenied
	}
	return s.setAdministrator(ctx, accountUUID, actor.AccountUUID, granted, actor.AccountUUID)
}

func (s *Service) setAdministrator(ctx context.Context, accountUUID, operator string, granted bool, actorUUID string) error {
	if _, err := uuid.Parse(accountUUID); err != nil {
		return invalid("账号 UUID 无效")
	}
	if operator == "" {
		return invalid("必须填写运维操作者")
	}
	return s.db.WithPlatformAdminTx(ctx, func(tx *db.PlatformAdminTx) error {
		if err := tx.LockAdministration(ctx); err != nil {
			return err
		}
		if actorUUID != "" {
			actor, err := tx.Account(ctx, actorUUID)
			if errors.Is(err, db.ErrNotFound) || (err == nil && !actor.Granted) {
				return ErrDenied
			}
			if err != nil {
				return err
			}
			if err := lockActor(ctx, tx, actorUUID); err != nil {
				return err
			}
		}
		account, err := tx.Account(ctx, accountUUID)
		if err != nil {
			return err
		}
		if account.Granted == granted {
			return nil
		}
		if !granted {
			if _, err := tx.LockGrant(ctx, accountUUID); err != nil {
				return err
			}
			count, err := tx.CountAdmins(ctx)
			if err != nil {
				return err
			}
			if count <= 1 {
				return ErrLastAdmin
			}
		}
		return tx.SetGrant(ctx, db.PlatformAdminGrant{AccountUUID: accountUUID, Operator: operator}, granted)
	})
}

func (s *Service) Administrators(ctx context.Context) ([]db.PlatformAdminAccount, error) {
	return s.db.ListPlatformAdmins(ctx)
}
