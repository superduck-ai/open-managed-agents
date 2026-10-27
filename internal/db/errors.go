package db

import "errors"

var ErrLastOrganizationAdmin = errors.New("last organization administrator")

var ErrSessionBudgetReached = errors.New("session budget reached")
