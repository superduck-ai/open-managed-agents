package platform

import "time"

type UserRecord struct {
	UUID          string
	ExternalID    string
	Email         string
	FullName      *string
	DisplayName   *string
	IsVerified    bool
	AgeIsVerified bool
	Settings      map[string]any
	CreatedAt     time.Time
}

type OrganizationRecord struct {
	UUID                   string
	ExternalID             string
	Name                   string
	Domain                 *string
	ParentOrganizationUUID *string
	Settings               map[string]any
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type UserOrganizationRecord struct {
	OrganizationRecord
	Role    string
	AddedAt time.Time
}

type OrganizationUpdatePatch struct {
	Name     *string
	Settings map[string]any
}
