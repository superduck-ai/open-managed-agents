package platformadmin

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
)

type Organization struct {
	UUID           string    `json:"uuid"`
	Name           string    `json:"name"`
	Website        string    `json:"website"`
	Industry       string    `json:"industry"`
	Version        int64     `json:"version"`
	MemberCount    int64     `json:"member_count"`
	WorkspaceCount int64     `json:"workspace_count"`
	AdminCount     int64     `json:"admin_count"`
	CreatedAt      time.Time `json:"created_at"`
}

func organizationDTO(r db.PlatformAdminOrganization) Organization {
	return Organization{r.UUID, r.Name, r.Website, r.Industry, r.Version, r.MemberCount, r.WorkspaceCount, r.AdminCount, r.CreatedAt}
}

type CreateOrganization struct {
	Name              string   `json:"name"`
	AdminAccountUUIDs []string `json:"admin_account_uuids"`
	Reason            string   `json:"reason,omitempty"`
}

type Change struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason,omitempty"`
}

type OrganizationPatch struct {
	Change
	Name     *string `json:"name"`
	Website  *string `json:"website"`
	Industry *string `json:"industry"`
}

type AdministratorChange struct {
	Change
	AccountUUID        string `json:"account_uuid"`
	Role               string `json:"role"`
	PreviousMemberUUID string `json:"previous_member_uuid"`
}

func (s *Service) CreateOrganization(ctx context.Context, actor Actor, input CreateOrganization, key string) (Organization, bool, error) {
	var result Organization
	if input.Name == "" || len(input.Name) > 200 || key == "" || len(key) > 200 {
		return result, false, invalid("组织名称与幂等键为必填项，且不能超出长度限制")
	}
	if len(input.AdminAccountUUIDs) == 0 || len(input.AdminAccountUUIDs) > 20 {
		return result, false, invalid("请选择 1 至 20 位初始管理员")
	}
	for _, account := range input.AdminAccountUUIDs {
		if _, err := uuid.Parse(account); err != nil {
			return result, false, invalid("管理员账号 UUID 无效")
		}
	}
	input.AdminAccountUUIDs = slices.Compact(slices.Sorted(slices.Values(input.AdminAccountUUIDs)))
	body, err := json.Marshal(input)
	if err != nil {
		return result, false, err
	}
	replayed := false
	err = s.mutate(ctx, actor, func(tx *db.PlatformAdminTx) error {
		cached, found, err := tx.CreateRequest(ctx, actor.AccountUUID, key)
		if err != nil {
			return err
		}
		if found {
			if cached.Hash != auth.HashSecret(string(body)) {
				return ErrIdempotency
			}
			replayed = true
			return json.Unmarshal(cached.Response, &result)
		}
		workspaceID, err := ids.New("wrkspc_")
		if err != nil {
			return err
		}
		org, err := tx.CreateOrganization(ctx, input.Name, db.PlatformAuthWorkspaceInput{UUID: uuid.NewV4().String(), ExternalID: workspaceID, Name: "default", CompartmentID: uuid.NewV4().String()})
		if err != nil {
			return err
		}
		for _, account := range input.AdminAccountUUIDs {
			if _, err := grantOrganizationAdmin(ctx, tx, org, account); err != nil {
				return err
			}
		}
		row, err := tx.Organization(ctx, org)
		if err != nil {
			return err
		}
		result = organizationDTO(row)
		response, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.SaveCreateRequest(ctx, db.PlatformAdminCreateRequest{AccountUUID: actor.AccountUUID, Key: key, Hash: auth.HashSecret(string(body)), Response: response})
	})
	return result, replayed, err
}

func grantOrganizationAdmin(ctx context.Context, tx *db.PlatformAdminTx, org, accountUUID string) (db.PlatformAdminMember, error) {
	account, err := tx.Account(ctx, accountUUID)
	if err != nil {
		return db.PlatformAdminMember{}, err
	}
	member, found, err := tx.MemberByEmail(ctx, org, account.Email)
	if err != nil {
		return member, err
	}
	if found {
		if member.Role != "admin" {
			err = tx.SetMemberRole(ctx, org, member.UUID, "admin")
		}
		return member, err
	}
	member = db.PlatformAdminMember{UUID: uuid.NewV4().String(), Email: account.Email, Name: account.Name, Role: "admin"}
	err = tx.InsertMember(ctx, db.PlatformAdminMemberInput{OrganizationUUID: org, UUID: member.UUID,
		ExternalID: "user_" + strings.ReplaceAll(member.UUID, "-", "")[:24], Email: member.Email, Name: member.Name, Role: "admin"})
	return member, err
}

func (s *Service) changeOrganization(ctx context.Context, actor Actor, org string, input Change, change func(*db.PlatformAdminTx) error) (Organization, error) {
	var result Organization
	if input.ExpectedVersion < 1 {
		return result, invalid("必须填写有效版本")
	}
	err := s.mutate(ctx, actor, func(tx *db.PlatformAdminTx) error {
		version, err := tx.LockOrganization(ctx, org)
		if err != nil {
			return err
		}
		if version != input.ExpectedVersion {
			return ErrVersion
		}
		if err := change(tx); err != nil {
			return err
		}
		after, err := tx.Organization(ctx, org)
		if err != nil {
			return err
		}
		result = organizationDTO(after)
		return nil
	})
	return result, err
}

func (s *Service) UpdateOrganization(ctx context.Context, actor Actor, org string, input OrganizationPatch) (Organization, error) {
	if input.Name != nil && (*input.Name == "" || len(*input.Name) > 200) {
		return Organization{}, invalid("组织名称不能为空或超过 200 字节")
	}
	if input.Website != nil && len(*input.Website) > 2000 || input.Industry != nil && len(*input.Industry) > 200 {
		return Organization{}, invalid("组织资料超出长度限制")
	}
	if input.Name == nil && input.Website == nil && input.Industry == nil {
		return Organization{}, invalid("没有要更新的资料")
	}
	return s.changeOrganization(ctx, actor, org, input.Change, func(tx *db.PlatformAdminTx) error {
		return tx.UpdateOrganization(ctx, db.PlatformAdminOrganizationPatch{OrganizationUUID: org, Name: input.Name, Website: input.Website, Industry: input.Industry})
	})
}

func (s *Service) GrantAdministrator(ctx context.Context, actor Actor, org string, input AdministratorChange) (Organization, error) {
	if _, err := uuid.Parse(input.AccountUUID); err != nil {
		return Organization{}, invalid("管理员账号 UUID 无效")
	}
	return s.changeOrganization(ctx, actor, org, input.Change, func(tx *db.PlatformAdminTx) error {
		_, err := grantOrganizationAdmin(ctx, tx, org, input.AccountUUID)
		return err
	})
}

func (s *Service) DemoteAdministrator(ctx context.Context, actor Actor, org, member string, input AdministratorChange) (Organization, error) {
	if !nonAdminRole(input.Role) {
		return Organization{}, invalid("请选择有效的非管理员角色")
	}
	return s.changeOrganization(ctx, actor, org, input.Change, func(tx *db.PlatformAdminTx) error {
		return demoteAdministrator(ctx, tx, org, member, input.Role)
	})
}

func demoteAdministrator(ctx context.Context, tx *db.PlatformAdminTx, org, memberUUID, role string) error {
	member, err := tx.Member(ctx, org, memberUUID)
	if err != nil {
		return err
	}
	if member.Role != "admin" {
		return ErrState
	}
	count, err := tx.CountOrganizationAdmins(ctx, org)
	if err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastOrganizationAdmin
	}
	return tx.SetMemberRole(ctx, org, memberUUID, role)
}

func (s *Service) TransferAdministrator(ctx context.Context, actor Actor, org string, input AdministratorChange) (Organization, error) {
	if !nonAdminRole(input.Role) {
		return Organization{}, invalid("请选择原管理员的新角色")
	}
	if _, err := uuid.Parse(input.AccountUUID); err != nil {
		return Organization{}, invalid("新管理员账号 UUID 无效")
	}
	if _, err := uuid.Parse(input.PreviousMemberUUID); err != nil {
		return Organization{}, invalid("原管理员成员 UUID 无效")
	}
	return s.changeOrganization(ctx, actor, org, input.Change, func(tx *db.PlatformAdminTx) error {
		member, err := grantOrganizationAdmin(ctx, tx, org, input.AccountUUID)
		if err != nil {
			return err
		}
		if member.UUID == input.PreviousMemberUUID {
			return invalid("新旧管理员不能是同一个账号")
		}
		return demoteAdministrator(ctx, tx, org, input.PreviousMemberUUID, input.Role)
	})
}

func nonAdminRole(role string) bool {
	return role == "user"
}
