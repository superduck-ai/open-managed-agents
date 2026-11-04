package platformadmin

import (
	"context"
	"net/http"
	"strings"
	"uuid"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type MemberChange struct {
	Change
	AccountUUID string `json:"account_uuid"`
	Role        string `json:"role"`
}

func (s *Service) AddMember(ctx context.Context, actor Actor, org string, input MemberChange) (Organization, error) {
	if input.Role != "user" && input.Role != "admin" {
		return Organization{}, invalid("请选择 User 或 Admin 组织角色")
	}
	if _, err := uuid.Parse(input.AccountUUID); err != nil {
		return Organization{}, invalid("账号 UUID 无效")
	}
	return s.changeOrganization(ctx, actor, org, input.Change, func(tx *db.PlatformAdminTx) error {
		account, err := tx.Account(ctx, input.AccountUUID)
		if err != nil {
			return err
		}
		_, found, err := tx.MemberByEmail(ctx, org, account.Email)
		if err != nil {
			return err
		}
		if found {
			return ErrMemberExists
		}
		memberUUID := uuid.NewV4().String()
		return tx.InsertMember(ctx, db.PlatformAdminMemberInput{OrganizationUUID: org, UUID: memberUUID, ExternalID: "user_" + strings.ReplaceAll(memberUUID, "-", "")[:24], Email: account.Email, Name: account.Name, Role: input.Role})
	})
}

func (s *Service) ChangeMember(ctx context.Context, actor Actor, org, member string, input MemberChange, remove bool) (Organization, error) {
	if !remove && input.Role != "user" && input.Role != "admin" {
		return Organization{}, invalid("请选择 User 或 Admin 组织角色")
	}
	return s.changeOrganization(ctx, actor, org, input.Change, func(tx *db.PlatformAdminTx) error {
		target, err := tx.Member(ctx, org, member)
		if err != nil {
			return err
		}
		if target.Role == "admin" && (remove || input.Role != "admin") {
			count, err := tx.CountOrganizationAdmins(ctx, org)
			if err != nil {
				return err
			}
			if count <= 1 {
				return ErrLastOrganizationAdmin
			}
		}
		if remove {
			return tx.RemoveMember(ctx, org, member)
		}
		return tx.SetMemberRole(ctx, org, member, input.Role)
	})
}

func (h *Handler) addMember(w http.ResponseWriter, r *http.Request) {
	input, err := decodeRequest[MemberChange](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	result, err := h.service.AddMember(r.Context(), actorFromRequest(r), chi.URLParam(r, "organizationUuid"), input)
	h.writeChange(w, r, result, err)
}

func (h *Handler) updateMember(w http.ResponseWriter, r *http.Request) { h.changeMember(w, r, false) }
func (h *Handler) removeMember(w http.ResponseWriter, r *http.Request) { h.changeMember(w, r, true) }
func (h *Handler) changeMember(w http.ResponseWriter, r *http.Request, remove bool) {
	input, err := decodeRequest[MemberChange](w, r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	member := chi.URLParam(r, "memberUuid")
	if _, err := uuid.Parse(member); err != nil {
		h.fail(w, r, invalid("成员 UUID 无效"))
		return
	}
	result, err := h.service.ChangeMember(r.Context(), actorFromRequest(r), chi.URLParam(r, "organizationUuid"), member, input, remove)
	h.writeChange(w, r, result, err)
}
