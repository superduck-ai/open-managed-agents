package db

import "errors"

var ErrSessionEventConflict = errors.New("session event ID conflicts with previously accepted content")

var ErrCodeSessionInternalEventConflict = errors.New("internal event identity conflicts with previously accepted content")
