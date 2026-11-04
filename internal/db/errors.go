package db

import "errors"

var ErrLastOrganizationAdmin = errors.New("last organization administrator")

var ErrSessionBudgetReached = errors.New("session budget reached")

var (
	ErrDSHHeaderRequired     = errors.New("DSH transcript header required")
	ErrDSHTranscriptConflict = errors.New("DSH transcript conflict")
	ErrDSHTranscriptGap      = errors.New("DSH transcript sequence gap")
)

var ErrDSHInputNotEligible = errors.New("DSR input is not eligible")
var ErrDSHFinishRequired = errors.New("DSR finish is required before ACK")
var ErrDSHFinishConflict = errors.New("DSR finish differs from confirmed result")
var ErrDSHPermissionConflict = errors.New("DSR permission identity mismatch")
var ErrDSHPublicConflict = errors.New("DSR public event identity mismatch")

var ErrDSHControlConflict = errors.New("DSR control identity mismatch")
