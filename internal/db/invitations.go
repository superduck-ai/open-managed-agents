package db

import (
	"context"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/yourbatis"
)

var (
	ErrInvitationConflict = errors.New("invitation state conflicts with the requested action")
	ErrInvitationExpired  = errors.New("invitation expired")
	ErrInvitationRevoked  = errors.New("invitation revoked")
)

type Invitation struct {
	ID               string
	OrganizationUUID string
	OrganizationName string
	Email            string
	Role             string
	Status           string
	InvitedAt        time.Time
	ExpiresAt        time.Time
	UserID           string
}

func (r invitationRow) invitation() Invitation {
	return Invitation{ID: r.ID, OrganizationUUID: r.OrganizationUUID, OrganizationName: r.OrganizationName,
		Email: r.Email, Role: r.Role, Status: r.Status, InvitedAt: r.InvitedAt, ExpiresAt: r.ExpiresAt}
}

func (d *DB) ListInvitations(ctx context.Context, email string) ([]Invitation, error) {
	rows, err := NewInvitationMapper(d.mapperDB).ListByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	result := make([]Invitation, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.invitation())
	}
	return result, nil
}

// RespondToInvitation 在同一事务中锁定邀请、复用或创建组织成员并转换状态。
func (d *DB) RespondToInvitation(ctx context.Context, id, email string, accept bool) (Invitation, error) {
	var result Invitation
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewInvitationMapper(executor)
		row, found, err := mapper.LockByIDAndEmail(ctx, id, email)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		result = row.invitation()
		if row.Status == "deleted" {
			return ErrInvitationRevoked
		}
		if row.Status == "expired" || (row.Status == "pending" && !time.Now().Before(row.ExpiresAt)) {
			return ErrInvitationExpired
		}
		target := "declined"
		if accept {
			target = "accepted"
		}
		if row.Status != "pending" && row.Status != target {
			return ErrInvitationConflict
		}
		if accept {
			member, err := ensureInvitationMember(ctx, mapper, row)
			if err != nil {
				return err
			}
			result.UserID, result.Role = member.ID, member.Role
		}
		if row.Status == target {
			return nil
		}
		count, err := mapper.UpdateStatus(ctx, row.OrganizationUUID, id, target)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrInvitationExpired
		}
		result.Status = target
		return nil
	})
	return result, err
}

func ensureInvitationMember(ctx context.Context, mapper InvitationMapper, row invitationRow) (invitationMemberRow, error) {
	member, found, err := mapper.FindActiveMember(ctx, row.OrganizationUUID, row.Email)
	if err != nil || found {
		return member, err
	}
	// 已接受邀请不得重建被移除的组织成员。
	if row.Status == "accepted" {
		return invitationMemberRow{}, ErrInvitationConflict
	}
	id, err := ids.New("user_")
	if err != nil {
		return invitationMemberRow{}, err
	}
	if _, err = mapper.InsertMember(ctx, invitationMemberParams{ID: id, OrganizationUUID: row.OrganizationUUID, Email: row.Email, Role: row.Role}); err != nil {
		return invitationMemberRow{}, err
	}
	// 不同邀请可并发命中同一邮箱；独立 SELECT 读取唯一索引冲突后已提交的成员。
	member, found, err = mapper.FindActiveMember(ctx, row.OrganizationUUID, row.Email)
	if err != nil {
		return invitationMemberRow{}, err
	}
	if !found {
		return invitationMemberRow{}, ErrInvitationConflict
	}
	return member, nil
}
