package platformadmin

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

type Service struct {
	db     *db.DB
	logger *slog.Logger
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
		a.Permissions = []string{"platform:access", "platform:organizations:read", "platform:organizations:write", "platform:organization-admins:manage"}
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
		granted, err := tx.LockGrant(ctx, actor.AccountUUID)
		if errors.Is(err, db.ErrNotFound) {
			return ErrDenied
		}
		if err != nil {
			return err
		}
		if !granted {
			return ErrDenied
		}
		return fn(tx)
	})
}
func (s *Service) SetAdministrator(ctx context.Context, accountUUID, operator string, _ string, granted bool) error {
	if _, err := uuid.Parse(accountUUID); err != nil {
		return invalid("账号 UUID 无效")
	}
	operator = strings.TrimSpace(operator)
	if operator == "" {
		return invalid("必须填写运维操作者")
	}
	return s.db.WithPlatformAdminTx(ctx, func(tx *db.PlatformAdminTx) error {
		if err := tx.LockAdministration(ctx); err != nil {
			return err
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
