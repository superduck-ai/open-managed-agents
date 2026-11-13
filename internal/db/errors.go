package db

import "errors"

var ErrLastOrganizationAdmin = errors.New("last organization administrator")

var ErrSessionBudgetReached = errors.New("session budget reached")

var ErrChannelCredentialInactive = errors.New("channel workspace credential is inactive")

var ErrChannelCredentialUnreadable = errors.New("channel workspace credential cannot be decrypted")

var ErrToolCallConflict = errors.New("tool call conflicts with persisted state")

var ErrToolCallSandboxPreparing = errors.New("tool call sandbox is still being prepared")

var ErrToolCallSandboxRetired = errors.New("tool call sandbox no longer accepts calls")
