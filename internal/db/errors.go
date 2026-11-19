package db

import "errors"

var ErrBundledSkillReadOnly = errors.New("bundled skill cannot be removed")

var ErrLastOrganizationAdmin = errors.New("last organization administrator")

var ErrSessionUpdateConflict = errors.New("session changed during update")

var ErrSessionBudgetNotRaised = errors.New("budget.max_list_cost must be greater than the session's consumed list cost")

var ErrSessionBudgetReached = errors.New("session budget reached")

var ErrChannelCredentialInactive = errors.New("channel workspace credential is inactive")

var ErrChannelCredentialUnreadable = errors.New("channel workspace credential cannot be decrypted")

var ErrToolCallConflict = errors.New("tool call conflicts with persisted state")

var ErrToolCallSandboxPreparing = errors.New("tool call sandbox is still being prepared")

var ErrToolCallSandboxRetired = errors.New("tool call sandbox no longer accepts calls")
var ErrChannelCredentialUserRequired = errors.New("channel workspace credential requires a runtime user")
