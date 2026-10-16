package platformadmin

import (
	"context"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

type InviteChange struct {
	Change
	Email string `json:"email"`
	Role  string `json:"role"`
}

type Invite struct {
	UUID           string    `json:"uuid"`
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	InvitedAt      time.Time `json:"invited_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	DeliveryStatus string    `json:"delivery_status"`
}

func (s *Service) CreateInvite(ctx context.Context, actor Actor, org string, input InviteChange) (Organization, error) {
	if input.Role != "user" && input.Role != "admin" {
		return Organization{}, invalid("请选择 User 或 Admin 组织角色")
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 254 {
		return Organization{}, invalid("请输入有效的邮箱地址")
	}
	return s.changeOrganization(ctx, actor, org, input.Change, func(tx *db.PlatformAdminTx) error {
		if err := checkInviteRecipient(ctx, tx, org, input.Email); err != nil {
			return err
		}
		id, err := ids.New("invite_")
		if err != nil {
			return err
		}
		_, err = tx.InsertInvite(ctx, db.PlatformAdminInviteInput{UUID: uuid.NewV4().String(), ExternalID: id, OrganizationUUID: org, AccountUUID: actor.AccountUUID, Email: input.Email, Role: input.Role})
		return err
	})
}

func checkInviteRecipient(ctx context.Context, tx *db.PlatformAdminTx, org, email string) error {
	_, member, err := tx.MemberByEmail(ctx, org, email)
	if err != nil {
		return err
	}
	if member {
		return invalid("该邮箱已是组织成员，请在成员管理中调整角色")
	}
	pending, err := tx.HasPendingInvite(ctx, org, email)
	if err != nil {
		return err
	}
	if pending {
		return ErrState
	}
	return nil
}

func (s *Service) ChangeInvite(ctx context.Context, actor Actor, org, invitation string, input Change, resend bool) (Organization, error) {
	if _, err := uuid.Parse(invitation); err != nil {
		return Organization{}, invalid("邀请 UUID 无效")
	}
	return s.changeOrganization(ctx, actor, org, input, func(tx *db.PlatformAdminTx) error {
		row, err := tx.Invite(ctx, org, invitation)
		if err != nil {
			return err
		}
		if row.Status != "pending" && row.Status != "expired" {
			return ErrState
		}
		if !resend {
			return tx.RevokeInvite(ctx, org, invitation)
		}
		if time.Since(row.InvitedAt) < time.Minute {
			return ErrRateLimited
		}
		_, member, err := tx.MemberByEmail(ctx, org, row.Email)
		if err != nil {
			return err
		}
		if member {
			return invalid("该邮箱已是组织成员，请在成员管理中调整角色")
		}
		return tx.ResendInvite(ctx, org, invitation)
	})
}

func (h *Handler) invites(w http.ResponseWriter, r *http.Request) {
	query, err := readPage(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	rows, err := h.service.db.ListPlatformAdminInvites(r.Context(), query)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writePage(w, rows, query.Limit, func(row db.PlatformAdminInvite) Invite {
		return Invite{row.UUID, row.ExternalID, row.Email, row.Role, row.Status, row.InvitedAt, row.ExpiresAt, row.DeliveryStatus}
	}, func(row db.PlatformAdminInvite) string { return row.UUID })
}

func (h *Handler) createInvite(w http.ResponseWriter, r *http.Request) {
	input, err := decodeRequest[InviteChange](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	cleanChange(&input.Change)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	result, err := h.service.CreateInvite(r.Context(), actorFromRequest(r), chi.URLParam(r, "organizationUuid"), input)
	h.writeChange(w, r, result, err)
}

func (h *Handler) changeInvite(resend bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, err := decodeRequest[Change](w, r)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		cleanChange(&input)
		result, err := h.service.ChangeInvite(r.Context(), actorFromRequest(r), chi.URLParam(r, "organizationUuid"), chi.URLParam(r, "invitationUuid"), input, resend)
		h.writeChange(w, r, result, err)
	}
}
