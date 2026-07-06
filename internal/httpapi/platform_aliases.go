package httpapi

import "github.com/superduck-ai/open-managed-agents/internal/platform"

var ErrNotFound = platform.ErrNotFound

type UserRecord = platform.UserRecord
type OrganizationRecord = platform.OrganizationRecord
type UserOrganizationRecord = platform.UserOrganizationRecord
type OrganizationUpdatePatch = platform.OrganizationUpdatePatch

type AdminRequest = platform.AdminRequest

type ConsoleWorkspace = platform.ConsoleWorkspace
type ConsoleWorkspaceDataResidency = platform.ConsoleWorkspaceDataResidency
type CreateConsoleWorkspaceInput = platform.CreateConsoleWorkspaceInput
type ConsoleAPIKey = platform.ConsoleAPIKey
type CreateConsoleAPIKeyInput = platform.CreateConsoleAPIKeyInput
type CreateConsoleAPIKeyResult = platform.CreateConsoleAPIKeyResult
type UpdateConsoleAPIKeyStatusInput = platform.UpdateConsoleAPIKeyStatusInput
type ConsoleInvite = platform.ConsoleInvite
type CreateConsoleInviteInput = platform.CreateConsoleInviteInput
type OrgUser = platform.OrgUser

type OrganizationPhysicalAddress = platform.OrganizationPhysicalAddress
type OrganizationTaxID = platform.OrganizationTaxID
type OrganizationProfile = platform.OrganizationProfile

type WorkbenchPromptRecord = platform.WorkbenchPromptRecord
type WorkbenchRevisionRecord = platform.WorkbenchRevisionRecord
type WorkbenchKVRecord = platform.WorkbenchKVRecord
type WorkbenchEvaluationRecord = platform.WorkbenchEvaluationRecord
