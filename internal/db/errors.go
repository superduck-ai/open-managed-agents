package db

import "errors"

var ErrBundledSkillReadOnly = errors.New("bundled skill cannot be removed")

var ErrLastOrganizationAdmin = errors.New("last organization administrator")

var ErrSessionBudgetReached = errors.New("session budget reached")

var ErrChannelCredentialInactive = errors.New("channel workspace credential is inactive")

var ErrChannelCredentialUnreadable = errors.New("channel workspace credential cannot be decrypted")
